package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/zopping/gateway-svc/accountclient"
	"github.com/zopping/gateway-svc/config"
)

// fakeAccounts answers the gateway's calls to account-svc from a table of tokens, and records what was asked.
type fakeAccounts struct {
	mu         sync.Mutex
	principals map[string]accountclient.Principal
	down       bool
	checks     []string // "service method path"
	mes        int
}

func (f *fakeAccounts) lookup(token string) (accountclient.Principal, error) {
	if f.down {
		return accountclient.Principal{}, accountclient.ErrUnavailable
	}

	switch token {
	case "forbidden":
		return accountclient.Principal{}, &accountclient.Error{Status: 403, Body: []byte(`{"error":{"message":"not permitted"}}`)}
	case "locked":
		return accountclient.Principal{}, &accountclient.Error{Status: 429, Body: []byte(`{"error":{"message":"locked"}}`)}
	}

	p, ok := f.principals[token]
	if !ok {
		return accountclient.Principal{}, &accountclient.Error{Status: 401, Body: []byte(`{"error":{"message":"invalid or expired token"}}`)}
	}

	return p, nil
}

func (f *fakeAccounts) Check(_ context.Context, token, service, method, path string) (accountclient.Principal, error) {
	f.mu.Lock()
	f.checks = append(f.checks, service+" "+method+" "+path)
	f.mu.Unlock()

	return f.lookup(token)
}

func (f *fakeAccounts) Me(_ context.Context, token string) (accountclient.Principal, error) {
	f.mu.Lock()
	f.mes++
	f.mu.Unlock()

	return f.lookup(token)
}

// backend is a fake downstream service that records the last request it received.
type backend struct {
	srv    *httptest.Server
	mu     sync.Mutex
	calls  int
	last   *http.Request
	body   string
	status int
	delay  time.Duration
}

func newBackend(t *testing.T) *backend {
	t.Helper()

	b := &backend{status: 200}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)

		b.mu.Lock()
		b.calls++
		b.last, b.body = r.Clone(context.Background()), string(raw)
		status, delay := b.status, b.delay
		b.mu.Unlock()

		time.Sleep(delay)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Backend", "yes")
		w.Header().Set("Access-Control-Allow-Origin", "backend-says-so")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"data":"from backend"}`))
	}))
	t.Cleanup(b.srv.Close)

	return b
}

type rig struct {
	h         http.Handler
	accounts  *fakeAccounts
	catalogue *backend
	inventory *backend
	account   *backend
	settings  config.Settings
}

func newRig(t *testing.T, mutate ...func(*config.Settings)) *rig {
	t.Helper()

	r := &rig{catalogue: newBackend(t), inventory: newBackend(t), account: newBackend(t)}
	r.accounts = &fakeAccounts{principals: map[string]accountclient.Principal{
		"seller7": {User: accountclient.User{ID: 5, Attributes: map[string]any{"sellerId": 7.0}, RoleIDs: []int64{2, 3}}},
		"admin":   {User: accountclient.User{ID: 1, RoleIDs: []int64{1}}, AllAccess: true},
		"plain":   {User: accountclient.User{ID: 9}},
		"staff":   {User: accountclient.User{ID: 3, Attributes: map[string]any{"sellerScope": "all"}, RoleIDs: []int64{4}}},
	}}

	parse := func(b *backend) *url.URL { u, _ := url.Parse(b.srv.URL); return u }

	r.settings = config.Settings{
		Services:     map[string]*url.URL{"catalogue-svc": parse(r.catalogue), "inventory-svc": parse(r.inventory), "account-svc": parse(r.account)},
		ProxyTimeout: 2 * time.Second, MaxBodyBytes: 1 << 10, LoginPerMinute: 2,
	}

	for _, m := range mutate {
		m(&r.settings)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	r.h = New(r.settings, r.accounts).Middleware()(next)

	return r
}

func (r *rig) do(method, target, token, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)

	return w
}

func TestOnlyAPIPathsAreHandled(t *testing.T) {
	r := newRig(t)

	assert.Equal(t, http.StatusTeapot, r.do("GET", "/.well-known/health", "", "").Code, "GoFr's own routes pass through")
	assert.Equal(t, http.StatusNotFound, r.do("GET", "/api/other-svc/x", "t", "").Code, "unknown service")
	assert.Equal(t, http.StatusNotFound, r.do("GET", "/api/", "", "").Code)
	assert.Equal(t, http.StatusNoContent, r.do("OPTIONS", "/api/catalogue-svc/product", "", "").Code, "preflight needs no token")
	assert.Zero(t, r.catalogue.calls)
}

func TestProtectedRoutesNeedAValidTokenAndFailClosed(t *testing.T) {
	r := newRig(t)

	w := r.do("PUT", "/api/catalogue-svc/product/42", "", "")
	assert.Equal(t, 401, w.Code, "no token")
	assert.Contains(t, w.Body.String(), "missing token")

	assert.Equal(t, 401, r.do("PUT", "/api/catalogue-svc/product/42", "wrong", "").Code)
	assert.Contains(t, r.do("PUT", "/api/catalogue-svc/product/42", "wrong", "").Body.String(), "invalid or expired token",
		"account-svc's own answer is passed through")

	w = r.do("PUT", "/api/catalogue-svc/product/42", "forbidden", "")
	assert.Equal(t, 403, w.Code)
	assert.JSONEq(t, `{"error":{"message":"not permitted"}}`, w.Body.String())

	assert.Equal(t, 429, r.do("PUT", "/api/catalogue-svc/product/42", "locked", "").Code)

	r.accounts.down = true
	w = r.do("PUT", "/api/catalogue-svc/product/42", "admin", "")
	assert.Equal(t, 503, w.Code, "with authentication down nothing gets through, not even an admin")
	assert.Zero(t, r.catalogue.calls, "no request ever reached a backend without being authorised")

	for _, header := range []string{"Basic abc", "abc", "Bearer", "Bearer "} {
		assert.Equal(t, 401, r.do("GET", "/api/inventory-svc/stock", "", "", "Authorization", header).Code, header)
	}
}

func TestAnAuthorisedRequestIsForwardedWithVerifiedIdentity(t *testing.T) {
	r := newRig(t)

	w := r.do("PUT", "/api/inventory-svc/stock/1/seller/7/price?x=1&y=two", "seller7", `{"mrp": 10}`,
		"Cookie", "session=abc", "X-Custom", "kept")

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "yes", w.Header().Get("X-Backend"), "the backend's headers come back")
	assert.JSONEq(t, `{"data":"from backend"}`, w.Body.String())
	assert.NotEmpty(t, w.Header().Get("X-Correlation-Id"))
	assert.Empty(t, w.Header().Values("Access-Control-Allow-Origin"), "the backend's CORS headers are dropped; the gateway sets its own")

	assert.Equal(t, []string{"inventory-svc PUT /stock/1/seller/7/price"}, r.accounts.checks,
		"account-svc is asked about the backend's own path, not /api/...")

	got := r.inventory.last
	assert.Equal(t, "PUT", got.Method)
	assert.Equal(t, "/stock/1/seller/7/price", got.URL.Path, "/api/{service} is stripped")
	assert.Equal(t, "x=1&y=two", got.URL.RawQuery)
	assert.JSONEq(t, `{"mrp": 10}`, r.inventory.body)
	assert.Equal(t, "5", got.Header.Get("X-User-Id"))
	assert.Equal(t, "7", got.Header.Get("X-Seller-Id"))
	assert.Equal(t, "2,3", got.Header.Get("X-Role-Ids"))
	assert.Empty(t, got.Header.Get("X-All-Access"))
	assert.Empty(t, got.Header.Get("Authorization"), "the backends never see the token")
	assert.Empty(t, got.Header.Get("Cookie"))
	assert.Equal(t, "kept", got.Header.Get("X-Custom"))
	assert.NotEmpty(t, got.Header.Get("X-Forwarded-For"))
	assert.Equal(t, w.Header().Get("X-Correlation-Id"), got.Header.Get("X-Correlation-Id"))
	assert.Equal(t, 1, r.inventory.calls)
	assert.Zero(t, r.catalogue.calls)
}

func TestAdminIsForwardedAsAllAccessWithoutASeller(t *testing.T) {
	r := newRig(t)

	assert.Equal(t, 200, r.do("GET", "/api/catalogue-svc/seller-product-request", "admin", "").Code)

	got := r.catalogue.last
	assert.Equal(t, "1", got.Header.Get("X-User-Id"))
	assert.Equal(t, "true", got.Header.Get("X-All-Access"))
	assert.Equal(t, "all", got.Header.Get("X-Seller-Scope"))
	assert.Empty(t, got.Header.Get("X-Seller-Id"))
	assert.Empty(t, got.URL.Query().Get("sellerId"), "admins are not limited to one seller")
}

func TestStaffWithASellerScopeAreForwardedAsUnrestrictedButNotAdmin(t *testing.T) {
	r := newRig(t)

	assert.Equal(t, 200, r.do("GET", "/api/catalogue-svc/seller-product-request", "staff", "").Code)

	got := r.catalogue.last
	assert.Equal(t, "3", got.Header.Get("X-User-Id"))
	assert.Equal(t, "all", got.Header.Get("X-Seller-Scope"), "backends do not limit them to one seller")
	assert.Empty(t, got.Header.Get("X-All-Access"), "yet they are not admins")
	assert.Empty(t, got.URL.Query().Get("sellerId"), "so the list is every seller's")

	assert.Equal(t, 200, r.do("PUT", "/api/inventory-svc/stock/1/seller/8/price", "staff", `{}`).Code)
	assert.Equal(t, 403, r.do("PUT", "/api/account-svc/user/6/password", "staff", `{}`).Code, "and not other people's accounts")
}

func TestClientCannotForgeIdentity(t *testing.T) {
	r := newRig(t)

	forged := []string{
		"X-Seller-Id", "99", "x-user-id", "1", "X-ALL-ACCESS", "true", "X-Role-Ids", "1", "X-Identity-Admin", "yes",
		"X-Forwarded-User", "admin", "x-seller-scope", "all",
	}

	assert.Equal(t, 200, r.do("GET", "/api/catalogue-svc/product", "", "", forged...).Code, "a public route")

	got := r.catalogue.last
	for _, h := range []string{"X-Seller-Id", "X-User-Id", "X-All-Access", "X-Role-Ids", "X-Identity-Admin", "X-Forwarded-User", "X-Seller-Scope"} {
		assert.Empty(t, got.Header.Get(h), "%s must not reach the backend from an anonymous caller", h)
	}

	assert.Equal(t, 200, r.do("GET", "/api/catalogue-svc/seller-product-request", "seller7", "", forged...).Code)

	got = r.catalogue.last
	assert.Equal(t, "5", got.Header.Get("X-User-Id"), "the verified identity replaces the forged one")
	assert.Equal(t, "7", got.Header.Get("X-Seller-Id"))
	assert.Equal(t, "2,3", got.Header.Get("X-Role-Ids"))
	assert.Empty(t, got.Header.Get("X-All-Access"), "a seller cannot claim all access")
	assert.Empty(t, got.Header.Get("X-Seller-Scope"), "or the right to act across sellers")
	assert.Empty(t, got.Header.Get("X-Identity-Admin"))
}

func TestInternalRoutesAreNotReachable(t *testing.T) {
	r := newRig(t)

	for _, tc := range [][3]string{
		{"GET", "account-svc", "/auth/check?service=a&method=GET&path=/x"},
		{"PUT", "catalogue-svc", "/seller-product-request/reconcile"},
		{"PUT", "inventory-svc", "/reservation/release-expired"},
		{"PUT", "inventory-svc", "/stock/1/seller/7/offer"},
	} {
		w := r.do(tc[0], "/api/"+tc[1]+tc[2], "admin", `{"mrp":1}`)
		assert.Equal(t, 404, w.Code, "%s %s%s: even an admin gets a 404", tc[0], tc[1], tc[2])
	}

	assert.Empty(t, r.accounts.checks, "internal routes are refused before anyone is asked")
	assert.Zero(t, r.catalogue.calls+r.inventory.calls+r.account.calls)
}

func TestPublicRoutesNeedNoTokenAndAreLimitedToEnabledEntries(t *testing.T) {
	r := newRig(t)

	assert.Equal(t, 200, r.do("GET", "/api/catalogue-svc/product?status=HIDDEN&q=tea", "", "").Code)

	q := r.catalogue.last.URL.Query()
	assert.Equal(t, "ENABLED", q.Get("status"), "a client cannot list hidden products")
	assert.Equal(t, "tea", q.Get("q"))
	assert.Equal(t, []string{"ENABLED"}, q["status"])
	assert.Empty(t, r.accounts.checks, "no one is asked about a public route")

	assert.Equal(t, 200, r.do("GET", "/api/inventory-svc/stock/availability?variantIds=1,2", "", "").Code)
	assert.Equal(t, "/stock/availability", r.inventory.last.URL.Path)
	assert.Equal(t, 200, r.do("GET", "/api/catalogue-svc/product/42", "", "").Code)

	assert.Equal(t, 401, r.do("POST", "/api/catalogue-svc/product", "", `{"name":"x"}`).Code, "writing is never public")
	assert.Equal(t, 401, r.do("GET", "/api/inventory-svc/stock", "", "").Code, "the seller stock list is not public")
	assert.Equal(t, 401, r.do("GET", "/api/catalogue-svc/seller-product-request", "", "").Code)
}

func TestAuthOnlyRoutesCheckTheTokenButNotTheEndpoint(t *testing.T) {
	r := newRig(t)

	assert.Equal(t, 200, r.do("GET", "/api/account-svc/auth/me", "plain", "").Code, "a user with no roles can still ask who they are")
	assert.Equal(t, 1, r.accounts.mes)
	assert.Empty(t, r.accounts.checks, "no endpoint permission is needed")
	assert.Equal(t, "9", r.account.last.Header.Get("X-User-Id"))
	assert.Equal(t, "Bearer plain", r.account.last.Header.Get("Authorization"),
		"account-svc identifies the caller of /auth/me by the token itself, so it must be forwarded there")

	assert.Equal(t, 401, r.do("GET", "/api/account-svc/auth/me", "", "").Code)
	assert.Equal(t, 401, r.do("DELETE", "/api/account-svc/auth/session", "wrong", "").Code)
}

func TestGoogleLoginIsPublicAndSharesTheLoginRateLimit(t *testing.T) {
	r := newRig(t) // limit: 2 a minute

	post := func(path string) int {
		req := httptest.NewRequest("POST", path, strings.NewReader(`{"idToken":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "10.0.0.1:1111"

		w := httptest.NewRecorder()
		r.h.ServeHTTP(w, req)

		return w.Code
	}

	assert.Equal(t, 200, post("/api/account-svc/auth/google"), "reachable without a bearer token")
	assert.Equal(t, 200, post("/api/account-svc/auth/login"))
	assert.Equal(t, 429, post("/api/account-svc/auth/google"), "password and Google attempts draw on the same allowance")
	assert.Equal(t, 2, r.account.calls, "the limited attempt never reached account-svc")
}

func TestLoginIsPublicAndRateLimitedPerAddress(t *testing.T) {
	r := newRig(t) // limit: 2 a minute

	login := func(addr string) int {
		req := httptest.NewRequest("POST", "/api/account-svc/auth/login", strings.NewReader(`{"email":"a@b.co","password":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = addr

		w := httptest.NewRecorder()
		r.h.ServeHTTP(w, req)

		return w.Code
	}

	assert.Equal(t, 200, login("10.0.0.1:1111"))
	assert.Equal(t, 200, login("10.0.0.1:2222"), "the port does not matter")
	assert.Equal(t, 429, login("10.0.0.1:3333"))
	assert.Equal(t, 2, r.account.calls, "the limited attempt never reached account-svc")
	assert.Equal(t, 200, login("10.0.0.2:1111"), "another address is unaffected")

	// a spoofed X-Forwarded-For must not buy a fresh allowance unless a trusted proxy is configured
	req := httptest.NewRequest("POST", "/api/account-svc/auth/login", strings.NewReader(`{}`))
	req.Header.Set("X-Forwarded-For", "99.99.99.99")
	req.RemoteAddr = "10.0.0.1:4444"

	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)
	assert.Equal(t, 429, w.Code)

	trusted := newRig(t, func(s *config.Settings) { s.TrustForwarded = true })
	for i := 0; i < 2; i++ {
		req = httptest.NewRequest("POST", "/api/account-svc/auth/login", strings.NewReader(`{}`))
		req.Header.Set("X-Forwarded-For", "5.5.5.5, 10.0.0.1")
		req.RemoteAddr = "10.0.0.1:1"
		trusted.h.ServeHTTP(httptest.NewRecorder(), req)
	}

	req = httptest.NewRequest("POST", "/api/account-svc/auth/login", strings.NewReader(`{}`))
	req.Header.Set("X-Forwarded-For", "5.5.5.5")
	req.RemoteAddr = "10.0.0.9:1"

	w = httptest.NewRecorder()
	trusted.h.ServeHTTP(w, req)
	assert.Equal(t, 429, w.Code, "behind a trusted proxy the forwarded address is the one limited")
}

func TestSellersAreHeldToTheirOwnData(t *testing.T) {
	r := newRig(t)

	w := r.do("PUT", "/api/inventory-svc/stock/1/seller/8/price", "seller7", `{"mrp":10}`)
	assert.Equal(t, 403, w.Code)
	assert.Contains(t, w.Body.String(), "your own seller")
	assert.Zero(t, r.inventory.calls, "a refused request is never forwarded")

	assert.Equal(t, 200, r.do("PUT", "/api/inventory-svc/stock/1/seller/7/adjust", "seller7", `{"delta":1}`).Code)
	assert.Equal(t, 1, r.inventory.calls)

	assert.Equal(t, 200, r.do("GET", "/api/inventory-svc/stock", "seller7", "").Code)
	assert.Equal(t, "7", r.inventory.last.URL.Query().Get("sellerId"), "the list is limited to the caller's own seller")
	assert.Equal(t, 403, r.do("GET", "/api/inventory-svc/stock?sellerId=8", "seller7", "").Code)

	assert.Equal(t, 200, r.do("GET", "/api/inventory-svc/stock?sellerId=8", "admin", "").Code, "admins may look at any seller")
	assert.Equal(t, "8", r.inventory.last.URL.Query().Get("sellerId"))
	assert.Equal(t, 200, r.do("PUT", "/api/inventory-svc/stock/1/seller/8/price", "admin", `{"mrp":10}`).Code)

	w = r.do("GET", "/api/inventory-svc/stock/1/seller/7", "plain", "")
	assert.Equal(t, 403, w.Code)
	assert.Contains(t, w.Body.String(), "not linked to a seller")
}

func TestSellerBodyAttacksAreRefused(t *testing.T) {
	r := newRig(t)

	for _, body := range []string{`{"sellerId":8,"productId":1}`, `{"SELLERID":8,"productId":1}`, `{"sellerid":8}`, `{"sellerId":7,"SellerId":8}`} {
		w := r.do("POST", "/api/catalogue-svc/seller-product-request", "seller7", body)
		assert.Equal(t, 403, w.Code, body)
	}

	form := httptest.NewRequest("POST", "/api/catalogue-svc/seller-product-request", strings.NewReader("sellerId=8"))
	form.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	form.Header.Set("Authorization", "Bearer seller7")

	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, form)
	assert.Equal(t, 415, w.Code, "a form body cannot be inspected, so it is refused rather than trusted")

	big := `{"sellerId":7,"pad":"` + strings.Repeat("x", 4096) + `"}`
	assert.Equal(t, 413, r.do("POST", "/api/catalogue-svc/seller-product-request", "seller7", big).Code)

	assert.Zero(t, r.catalogue.calls, "none of the attacks reached the backend")

	assert.Equal(t, 200, r.do("POST", "/api/catalogue-svc/seller-product-request", "seller7", `{"sellerId":7,"productId":1}`).Code)
	assert.JSONEq(t, `{"sellerId":7,"productId":1}`, r.catalogue.body, "the backend gets the body intact")
	assert.Equal(t, 1, r.catalogue.calls)
}

func TestUsersMayOnlyManageThemselves(t *testing.T) {
	r := newRig(t)

	assert.Equal(t, 200, r.do("PUT", "/api/account-svc/user/5/password", "seller7", `{"currentPassword":"a","newPassword":"b"}`).Code)
	assert.Equal(t, 403, r.do("PUT", "/api/account-svc/user/6/password", "seller7", `{}`).Code, "someone else's password")
	assert.Equal(t, 200, r.do("PUT", "/api/account-svc/user/6/password", "admin", `{}`).Code)
}

func TestPathTricksCannotSidestepTheChecks(t *testing.T) {
	r := newRig(t)

	for _, path := range []string{
		"/api/inventory-svc/stock/1/seller/7/../8/price",
		"/api/inventory-svc/stock/1/seller/7/./price",
		"/api/inventory-svc/stock//1/seller/7/price",
		"/api/inventory-svc/stock/1/seller/7/%2e%2e/8/price",
		"/api/inventory-svc/stock/1/seller/7%2F..%2F8/price",
		"/api/inventory-svc/stock/1\\seller/7/price",
	} {
		w := r.do("PUT", path, "seller7", `{}`)
		assert.Equal(t, 400, w.Code, path)
	}

	assert.Zero(t, r.inventory.calls)
	assert.Empty(t, r.accounts.checks, "a malformed path is refused before authentication is even attempted")

	assert.Equal(t, 200, r.do("GET", "/api/catalogue-svc/product/", "", "").Code, "a trailing slash is tolerated")
	assert.Equal(t, "/product", r.catalogue.last.URL.Path)
}

func TestBackendFailures(t *testing.T) {
	r := newRig(t)

	r.catalogue.status = 409
	assert.Equal(t, 409, r.do("GET", "/api/catalogue-svc/product", "", "").Code, "the backend's status is passed through")

	r.catalogue.srv.Close()

	w := r.do("GET", "/api/catalogue-svc/product", "", "")
	assert.Equal(t, 502, w.Code)
	assert.Contains(t, w.Body.String(), "unavailable")

	slow := newRig(t, func(s *config.Settings) { s.ProxyTimeout = 100 * time.Millisecond })
	slow.inventory.delay = 400 * time.Millisecond

	assert.Equal(t, 504, slow.do("GET", "/api/inventory-svc/stock/availability?variantIds=1", "", "").Code)
}

func TestCorrelationIDIsKeptWhenGiven(t *testing.T) {
	r := newRig(t)

	w := r.do("GET", "/api/catalogue-svc/product", "", "", "X-Correlation-Id", "trace-123")
	assert.Equal(t, "trace-123", w.Header().Get("X-Correlation-Id"))
	assert.Equal(t, "trace-123", r.catalogue.last.Header.Get("X-Correlation-Id"))
}
