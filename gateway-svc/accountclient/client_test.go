package accountclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func server(t *testing.T, status int, body string, seen *http.Request) *HTTP {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen = *r.Clone(r.Context())
		}

		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	u, _ := url.Parse(srv.URL)

	return New(u)
}

const principal = `{"data":{"user":{"id":5,"attributes":{"sellerId":7},"roleIds":[2,3]},"allAccess":false}}`

func TestCheckSendsTheCallAndParsesThePrincipal(t *testing.T) {
	var seen http.Request

	c := server(t, 200, principal, &seen)

	p, err := c.Check(context.Background(), "tok", "catalogue-svc", "PUT", "/product/42")
	assert.NoError(t, err)
	assert.Equal(t, int64(5), p.User.ID)
	assert.Equal(t, []int64{2, 3}, p.User.RoleIDs)
	assert.False(t, p.AllAccess)

	id, ok := p.SellerID()
	assert.True(t, ok)
	assert.Equal(t, int64(7), id)

	assert.Equal(t, "/auth/check", seen.URL.Path)
	assert.Equal(t, "catalogue-svc", seen.URL.Query().Get("service"))
	assert.Equal(t, "PUT", seen.URL.Query().Get("method"))
	assert.Equal(t, "/product/42", seen.URL.Query().Get("path"))
	assert.Equal(t, "Bearer tok", seen.Header.Get("Authorization"), "the token goes in the header, never the URL")
}

func TestCheckEscapesAHostilePath(t *testing.T) {
	var seen http.Request

	c := server(t, 200, principal, &seen)

	_, err := c.Check(context.Background(), "tok", "svc", "GET", "/a?x=1&service=other-svc#frag")
	assert.NoError(t, err)
	assert.Equal(t, "svc", seen.URL.Query().Get("service"), "a path cannot smuggle in a different service parameter")
	assert.Equal(t, "/a?x=1&service=other-svc#frag", seen.URL.Query().Get("path"))
}

func TestMeCallsAuthMe(t *testing.T) {
	var seen http.Request

	c := server(t, 200, principal, &seen)

	_, err := c.Me(context.Background(), "tok")
	assert.NoError(t, err)
	assert.Equal(t, "/auth/me", seen.URL.Path)
}

func TestAccountRefusalsAreReturnedVerbatim(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusBadRequest} {
		c := server(t, status, `{"error":{"message":"nope"}}`, nil)

		_, err := c.Check(context.Background(), "tok", "svc", "GET", "/x")

		var refusal *Error

		assert.ErrorAs(t, err, &refusal, status)
		assert.Equal(t, status, refusal.Status)
		assert.JSONEq(t, `{"error":{"message":"nope"}}`, string(refusal.Body))
	}
}

func TestAnythingElseFailsClosed(t *testing.T) {
	for name, c := range map[string]*HTTP{
		"server error":       server(t, 500, "boom", nil),
		"bad gateway":        server(t, 502, "", nil),
		"200 but not JSON":   server(t, 200, "<html>", nil),
		"200 without a user": server(t, 200, `{"data":{}}`, nil),
		"200 with user 0":    server(t, 200, `{"data":{"user":{"id":0}}}`, nil),
		"200 with no data":   server(t, 200, `{}`, nil),
	} {
		_, err := c.Check(context.Background(), "tok", "svc", "GET", "/x")
		assert.ErrorIs(t, err, ErrUnavailable, name)
	}

	u, _ := url.Parse("http://127.0.0.1:1")

	_, err := New(u).Check(context.Background(), "tok", "svc", "GET", "/x")
	assert.ErrorIs(t, err, ErrUnavailable, "account-svc unreachable")
}

func TestAllSellers(t *testing.T) {
	with := func(scope any) Principal {
		return Principal{User: User{Attributes: map[string]any{"sellerScope": scope}}}
	}

	assert.True(t, Principal{AllAccess: true}.AllSellers(), "an admin")
	assert.True(t, with("all").AllSellers(), "staff with an explicit scope")

	for _, v := range []any{"ALL", "everyone", "", true, 1.0, nil, []any{"all"}} {
		assert.False(t, with(v).AllSellers(), "%v is not the scope \"all\"", v)
	}

	assert.False(t, Principal{User: User{Attributes: map[string]any{"sellerId": 7.0}}}.AllSellers(), "a seller is limited to their own")
	assert.False(t, Principal{}.AllSellers())
}

func TestSellerID(t *testing.T) {
	good := func(v any) Principal { return Principal{User: User{Attributes: map[string]any{"sellerId": v}}} }

	id, ok := good(12.0).SellerID()
	assert.True(t, ok)
	assert.Equal(t, int64(12), id)

	for _, v := range []any{"12", 0.0, -1.0, 1.5, nil, true, []any{12.0}} {
		_, ok := good(v).SellerID()
		assert.False(t, ok, "%v is not a seller id", v)
	}

	_, ok = Principal{}.SellerID()
	assert.False(t, ok, "no attributes at all")
}
