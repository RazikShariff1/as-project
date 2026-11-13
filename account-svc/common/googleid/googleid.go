// Package googleid verifies Google Sign-In ID tokens: the signed JWT that Google Identity Services hands the browser
// after the user picks an account. A token is trusted only if Google signed it (RS256, against Google's published keys),
// it was issued for one of our client IDs, it has not expired, and it carries an email.
package googleid

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/zopping/account-svc/common/apperr"
	"github.com/zopping/account-svc/models"
)

const (
	certsURL      = "https://www.googleapis.com/oauth2/v3/certs"
	invalidToken  = "invalid Google token"
	defaultKeyTTL = time.Hour
	// minRefresh bounds how often an unknown key id sends us back to Google, so junk tokens cannot make us hammer it.
	minRefresh   = time.Minute
	fetchTimeout = 5 * time.Second
	clockLeeway  = 30 * time.Second
	maxCertsSize = 1 << 20
	rsaExponent  = 65537
)

// Verifier checks Google ID tokens. It is safe for concurrent use.
type Verifier struct {
	audiences []string
	certsURL  string
	client    *http.Client
	now       func() time.Time

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	expires time.Time
	fetched time.Time
}

// New returns a Verifier that accepts tokens issued for any of the given OAuth client IDs.
func New(clientIDs ...string) *Verifier {
	return &Verifier{
		audiences: clientIDs,
		certsURL:  certsURL,
		client:    &http.Client{Timeout: fetchTimeout},
		now:       time.Now,
	}
}

// ParseClientIDs splits a comma-separated GOOGLE_CLIENT_ID value, dropping blanks.
func ParseClientIDs(raw string) []string {
	var ids []string

	for _, id := range strings.Split(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}

	return ids
}

// emailVerified reads Google's email_verified, which is a JSON boolean in ID tokens but a string in some other payloads.
type emailVerified bool

func (e *emailVerified) UnmarshalJSON(b []byte) error {
	*e = emailVerified(string(bytes.Trim(b, `"`)) == "true")

	return nil
}

type claims struct {
	jwt.RegisteredClaims
	Email         string        `json:"email"`
	EmailVerified emailVerified `json:"email_verified"`
	Name          string        `json:"name"`
}

// Verify returns who the token vouches for. A token that is forged, expired, for another app, or malformed is an
// *apperr.Unauthorized; a failure to reach Google is returned as a plain error (the caller answers 500, not 401).
func (v *Verifier) Verify(ctx context.Context, idToken string) (models.GoogleIdentity, error) {
	var c claims

	var keyErr error

	_, err := jwt.ParseWithClaims(idToken, &c, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)

		key, err := v.key(ctx, kid)
		if err != nil {
			keyErr = err

			return nil, err
		}

		return key, nil
	},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(clockLeeway),
		jwt.WithTimeFunc(v.now),
	)
	if err != nil {
		// A key-fetch failure is ours/Google's, not the caller's.
		if keyErr != nil && !isInvalid(keyErr) {
			return models.GoogleIdentity{}, keyErr
		}

		return models.GoogleIdentity{}, &apperr.Unauthorized{Reason: invalidToken}
	}

	if !validIssuer(c.Issuer) || !v.validAudience(c.Audience) || c.Subject == "" || c.Email == "" {
		return models.GoogleIdentity{}, &apperr.Unauthorized{Reason: invalidToken}
	}

	return models.GoogleIdentity{Subject: c.Subject, Email: c.Email, EmailVerified: bool(c.EmailVerified), Name: c.Name}, nil
}

func isInvalid(err error) bool {
	var u *apperr.Unauthorized

	return errors.As(err, &u)
}

func validIssuer(iss string) bool {
	return iss == "https://accounts.google.com" || iss == "accounts.google.com"
}

func (v *Verifier) validAudience(aud jwt.ClaimStrings) bool {
	for _, a := range aud {
		for _, want := range v.audiences {
			if a == want {
				return true
			}
		}
	}

	return false
}

// key returns Google's public key for a key id, refreshing the cached set when it has expired or the id is unknown.
func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if kid == "" {
		return nil, &apperr.Unauthorized{Reason: invalidToken}
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	now := v.now()
	if v.keys == nil || now.After(v.expires) {
		if err := v.refresh(ctx); err != nil {
			return nil, err
		}
	}

	if k, ok := v.keys[kid]; ok {
		return k, nil
	}

	// Google rotates keys: an id we do not know may simply be new. Look again, but not more than once a minute.
	if now.Sub(v.fetched) >= minRefresh {
		if err := v.refresh(ctx); err != nil {
			return nil, err
		}

		if k, ok := v.keys[kid]; ok {
			return k, nil
		}
	}

	return nil, &apperr.Unauthorized{Reason: invalidToken}
}

type jwks struct {
	Keys []struct {
		Kid string `json:"kid"`
		Kty string `json:"kty"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

// refresh downloads Google's signing keys. The caller holds v.mu.
func (v *Verifier) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.certsURL, http.NoBody)
	if err != nil {
		return err
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetching Google signing keys: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching Google signing keys: status %d", resp.StatusCode)
	}

	var set jwks
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxCertsSize)).Decode(&set); err != nil {
		return fmt.Errorf("reading Google signing keys: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(set.Keys))

	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}

		if pub, ok := rsaKey(k.N, k.E); ok {
			keys[k.Kid] = pub
		}
	}

	if len(keys) == 0 {
		return fmt.Errorf("Google returned no usable signing keys") //nolint:staticcheck // proper noun
	}

	now := v.now()
	v.keys, v.fetched, v.expires = keys, now, now.Add(keyTTL(resp.Header.Get("Cache-Control")))

	return nil
}

// keyTTL honours Google's Cache-Control max-age, falling back to an hour.
func keyTTL(cacheControl string) time.Duration {
	const marker = "max-age="

	if i := strings.Index(cacheControl, marker); i >= 0 {
		digits := cacheControl[i+len(marker):]
		if end := strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }); end >= 0 {
			digits = digits[:end]
		}

		if secs, err := strconv.Atoi(digits); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}

	return defaultKeyTTL
}

func rsaKey(n, e string) (*rsa.PublicKey, bool) {
	nb, err := base64.RawURLEncoding.DecodeString(n)
	if err != nil {
		return nil, false
	}

	eb, err := base64.RawURLEncoding.DecodeString(e)
	if err != nil {
		return nil, false
	}

	exp := int(new(big.Int).SetBytes(eb).Int64())
	if exp == 0 {
		exp = rsaExponent
	}

	return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: exp}, true
}
