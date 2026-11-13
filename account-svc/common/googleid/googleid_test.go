package googleid

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zopping/account-svc/common/apperr"
)

const (
	clientID = "624904373513-test.apps.googleusercontent.com"
	keyID    = "key-1"
	bits     = 2048
)

var fixedNow = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

// google is a fake of Google's signing-key endpoint plus a way to mint tokens it would accept.
type google struct {
	t       *testing.T
	key     *rsa.PrivateKey
	kids    atomic.Value // []string served by the endpoint
	fetches atomic.Int32
	server  *httptest.Server
	status  atomic.Int32
}

func newGoogle(t *testing.T, kids ...string) *google {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, bits)
	require.NoError(t, err)

	g := &google{t: t, key: key}
	g.kids.Store(kids)
	g.status.Store(http.StatusOK)

	g.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		g.fetches.Add(1)

		if st := int(g.status.Load()); st != http.StatusOK {
			w.WriteHeader(st)

			return
		}

		w.Header().Set("Cache-Control", "public, max-age=3600, must-revalidate")

		type jwk struct{ Kid, Kty, N, E string }

		var set struct{ Keys []jwk }

		for _, kid := range g.kids.Load().([]string) {
			set.Keys = append(set.Keys, jwk{
				Kid: kid, Kty: "RSA",
				N: base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			})
		}

		// Field names are lower-case in Google's payload.
		out := `{"keys":[`

		for i, k := range set.Keys {
			if i > 0 {
				out += ","
			}

			out += `{"kid":"` + k.Kid + `","kty":"RSA","alg":"RS256","use":"sig","n":"` + k.N + `","e":"` + k.E + `"}`
		}

		_, _ = w.Write([]byte(out + `]}`))
	}))
	t.Cleanup(g.server.Close)

	return g
}

func (g *google) verifier() *Verifier {
	v := New(clientID)
	v.certsURL = g.server.URL
	v.now = func() time.Time { return fixedNow }

	return v
}

type tokenOpts struct {
	kid      string
	aud      string
	iss      string
	email    string
	verified any
	exp      time.Time
	method   jwt.SigningMethod
	key      any
}

func (g *google) token(o tokenOpts) string {
	g.t.Helper()

	if o.kid == "" {
		o.kid = keyID
	}

	if o.aud == "" {
		o.aud = clientID
	}

	if o.iss == "" {
		o.iss = "https://accounts.google.com"
	}

	if o.email == "" {
		o.email = "Admin@Example.com"
	}

	if o.verified == nil {
		o.verified = true
	}

	if o.exp.IsZero() {
		o.exp = fixedNow.Add(time.Hour)
	}

	if o.method == nil {
		o.method = jwt.SigningMethodRS256
	}

	if o.key == nil {
		o.key = g.key
	}

	tok := jwt.NewWithClaims(o.method, jwt.MapClaims{
		"iss": o.iss, "aud": o.aud, "sub": "1234567890", "email": o.email, "email_verified": o.verified,
		"name": "Admin User", "iat": fixedNow.Add(-time.Minute).Unix(), "exp": o.exp.Unix(),
	})
	tok.Header["kid"] = o.kid

	signed, err := tok.SignedString(o.key)
	require.NoError(g.t, err)

	return signed
}

func TestVerify_ValidToken(t *testing.T) {
	g := newGoogle(t, keyID)

	id, err := g.verifier().Verify(context.Background(), g.token(tokenOpts{}))

	require.NoError(t, err)
	assert.Equal(t, "Admin@Example.com", id.Email, "the verifier reports the email as Google sent it; the service normalises it")
	assert.True(t, id.EmailVerified)
	assert.Equal(t, "1234567890", id.Subject)
	assert.Equal(t, "Admin User", id.Name)
}

func TestVerify_AcceptsOldIssuerSpelling(t *testing.T) {
	g := newGoogle(t, keyID)

	_, err := g.verifier().Verify(context.Background(), g.token(tokenOpts{iss: "accounts.google.com"}))

	assert.NoError(t, err)
}

func TestVerify_EmailVerifiedAsString(t *testing.T) {
	g := newGoogle(t, keyID)

	id, err := g.verifier().Verify(context.Background(), g.token(tokenOpts{verified: "true"}))
	require.NoError(t, err)
	assert.True(t, id.EmailVerified)

	id, err = g.verifier().Verify(context.Background(), g.token(tokenOpts{verified: false}))
	require.NoError(t, err)
	assert.False(t, id.EmailVerified, "an unverified email is reported, not rejected here; the service refuses it")
}

func TestVerify_RejectsBadTokens(t *testing.T) {
	g := newGoogle(t, keyID)

	other, err := rsa.GenerateKey(rand.Reader, bits)
	require.NoError(t, err)

	valid := g.token(tokenOpts{})
	parts := strings.Split(valid, ".")
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"https://accounts.google.com","aud":"`+clientID+`","sub":"1","email":"evil@example.com","exp":`+strconv.FormatInt(fixedNow.Add(time.Hour).Unix(), 10)+`}`)) + "." + parts[2]

	hs256 := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": clientID, "sub": "1", "email": "evil@example.com", "exp": fixedNow.Add(time.Hour).Unix(),
	})
	hs256.Header["kid"] = keyID
	// The classic algorithm-confusion attack: sign with HMAC using the public key bytes as the secret.
	hmacToken, err := hs256.SignedString(g.key.N.Bytes())
	require.NoError(t, err)

	none := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": clientID, "sub": "1", "email": "evil@example.com", "exp": fixedNow.Add(time.Hour).Unix(),
	})
	none.Header["kid"] = keyID
	noneToken, err := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	tests := map[string]string{
		"empty":                  "",
		"garbage":                "not-a-jwt",
		"signed by someone else": g.token(tokenOpts{key: other}),
		"payload tampered":       tampered,
		"wrong audience":         g.token(tokenOpts{aud: "999-someone-elses-app.apps.googleusercontent.com"}),
		"wrong issuer":           g.token(tokenOpts{iss: "https://evil.example.com"}),
		"expired":                g.token(tokenOpts{exp: fixedNow.Add(-time.Hour)}),
		"unknown key id":         g.token(tokenOpts{kid: "not-a-google-key"}),
		"hmac with public key":   hmacToken,
		"alg none":               noneToken,
	}

	for name, token := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := g.verifier().Verify(context.Background(), token)

			var u *apperr.Unauthorized

			assert.ErrorAs(t, err, &u, "must be a 401, never accepted and never a 500")
		})
	}
}

func TestVerify_KeysAreCachedAndRefreshedOnUnknownKid(t *testing.T) {
	g := newGoogle(t, keyID)
	v := g.verifier()

	for range 3 {
		_, err := v.Verify(context.Background(), g.token(tokenOpts{}))
		require.NoError(t, err)
	}

	assert.EqualValues(t, 1, g.fetches.Load(), "one download serves every token while the keys are fresh")

	// Google rotates: a token signed with a new key id arrives. Within a minute of the last fetch we do not refetch.
	g.kids.Store([]string{keyID, "key-2"})

	_, err := v.Verify(context.Background(), g.token(tokenOpts{kid: "key-2"}))
	assert.Error(t, err, "refetch is rate limited to once a minute")
	assert.EqualValues(t, 1, g.fetches.Load())

	// After a minute the unknown id sends us back to Google, which now serves it.
	v.now = func() time.Time { return fixedNow.Add(2 * time.Minute) }

	_, err = v.Verify(context.Background(), g.token(tokenOpts{kid: "key-2", exp: fixedNow.Add(time.Hour)}))
	assert.NoError(t, err)
	assert.EqualValues(t, 2, g.fetches.Load())
}

func TestVerify_ExpiredKeyCacheIsRefreshed(t *testing.T) {
	g := newGoogle(t, keyID)
	v := g.verifier()

	_, err := v.Verify(context.Background(), g.token(tokenOpts{}))
	require.NoError(t, err)

	v.now = func() time.Time { return fixedNow.Add(2 * time.Hour) } // past max-age=3600

	_, err = v.Verify(context.Background(), g.token(tokenOpts{exp: fixedNow.Add(3 * time.Hour)}))
	require.NoError(t, err)
	assert.EqualValues(t, 2, g.fetches.Load())
}

func TestVerify_GoogleUnreachableIsNotA401(t *testing.T) {
	g := newGoogle(t, keyID)
	g.status.Store(http.StatusBadGateway)

	_, err := g.verifier().Verify(context.Background(), g.token(tokenOpts{}))

	require.Error(t, err)

	var u *apperr.Unauthorized

	assert.False(t, errors.As(err, &u), "a Google outage is our 500, not the user's 401")
}

func TestVerify_MultipleClientIDs(t *testing.T) {
	g := newGoogle(t, keyID)
	v := New("other-client", clientID)
	v.certsURL, v.now = g.server.URL, func() time.Time { return fixedNow }

	_, err := v.Verify(context.Background(), g.token(tokenOpts{}))

	assert.NoError(t, err)
}

func TestParseClientIDs(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, ParseClientIDs(" a , b ,, "))
	assert.Empty(t, ParseClientIDs("  "))
	assert.Empty(t, ParseClientIDs(""))
}

func TestKeyTTL(t *testing.T) {
	assert.Equal(t, 3600*time.Second, keyTTL("public, max-age=3600, must-revalidate"))
	assert.Equal(t, 90*time.Second, keyTTL("max-age=90"))
	assert.Equal(t, defaultKeyTTL, keyTTL(""))
	assert.Equal(t, defaultKeyTTL, keyTTL("no-store"))
	assert.Equal(t, defaultKeyTTL, keyTTL("max-age=0"))
}

func TestEmailVerifiedUnmarshal(t *testing.T) {
	var c struct {
		V emailVerified `json:"v"`
	}

	for raw, want := range map[string]bool{`{"v":true}`: true, `{"v":"true"}`: true, `{"v":false}`: false, `{"v":"false"}`: false, `{}`: false} {
		c.V = false

		require.NoError(t, json.Unmarshal([]byte(raw), &c))
		assert.Equal(t, want, bool(c.V), raw)
	}
}
