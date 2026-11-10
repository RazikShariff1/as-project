package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/zopping/gateway-svc/accountclient"
	"github.com/zopping/gateway-svc/config"
	"github.com/zopping/gateway-svc/customerclient"
)

// fakeCustomers answers the gateway's calls to customer-svc from a table of tokens.
type fakeCustomers struct {
	shoppers map[string]customerclient.Principal
	down     bool
	asked    int
}

func (f *fakeCustomers) Me(_ context.Context, token string) (customerclient.Principal, error) {
	f.asked++

	if f.down {
		return customerclient.Principal{}, accountclient.ErrUnavailable
	}

	if token == "suspended" {
		return customerclient.Principal{}, &accountclient.Error{Status: 403, Body: []byte(`{"error":{"message":"account is suspended"}}`)}
	}

	p, ok := f.shoppers[token]
	if !ok {
		return customerclient.Principal{}, &accountclient.Error{Status: 401, Body: []byte(`{"error":{"message":"invalid or expired token"}}`)}
	}

	return p, nil
}

type storeRig struct {
	h         http.Handler
	customers *fakeCustomers
	accounts  *fakeAccounts
	customer  *backend
	order     *backend
	catalogue *backend
	inventory *backend
	account   *backend
}

func newStoreRig(t *testing.T) *storeRig {
	t.Helper()

	r := &storeRig{customer: newBackend(t), order: newBackend(t), catalogue: newBackend(t), inventory: newBackend(t), account: newBackend(t)}
	r.customers = &fakeCustomers{shoppers: map[string]customerclient.Principal{
		"asha": {Customer: customerclient.Customer{ID: 7, Status: "active"}},
	}}
	r.accounts = &fakeAccounts{principals: map[string]accountclient.Principal{"admin": {User: accountclient.User{ID: 1}, AllAccess: true}}}

	parse := func(b *backend) *url.URL { u, _ := url.Parse(b.srv.URL); return u }

	settings := config.Settings{
		Services: map[string]*url.URL{
			"customer-svc": parse(r.customer), "order-svc": parse(r.order), "catalogue-svc": parse(r.catalogue),
			"inventory-svc": parse(r.inventory), "account-svc": parse(r.account),
		},
		ProxyTimeout: 2 * time.Second, MaxBodyBytes: 1 << 10, LoginPerMinute: 2,
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	r.h = New(settings, r.accounts).WithCustomers(r.customers).Middleware()(next)

	return r
}

func (r *storeRig) do(method, target, token, body string, headers ...string) *httptest.ResponseRecorder {
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

func TestStoreIsOffUntilCustomersAreConfigured(t *testing.T) {
	settings := config.Settings{Services: map[string]*url.URL{}, ProxyTimeout: time.Second, MaxBodyBytes: 1 << 10, LoginPerMinute: 1}
	h := New(settings, &fakeAccounts{}).Middleware()(http.NotFoundHandler())

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/store/order-svc/order", nil))

	assert.Equal(t, 404, w.Code)
}

func TestStoreOnlyServesListedCalls(t *testing.T) {
	r := newStoreRig(t)

	// Not on the allowlist: the team's services, staff routes, unlisted methods and unknown services are all 404.
	for _, c := range [][2]string{
		{"GET", "/store/account-svc/user"},
		{"POST", "/store/account-svc/auth/login"},
		{"GET", "/store/customer-svc/customer"},
		{"GET", "/store/customer-svc/customer/3"},
		{"PUT", "/store/customer-svc/customer/3/status"},
		{"PUT", "/store/order-svc/order/ABC/status"},
		{"POST", "/store/order-svc/order-log"},
		{"DELETE", "/store/order-svc/order/ABC"},
		{"PUT", "/store/catalogue-svc/product/1"},
		{"GET", "/store/inventory-svc/stock"},
		{"PUT", "/store/inventory-svc/reservation/release-expired"},
		{"GET", "/store/nothing-svc/x"},
	} {
		assert.Equal(t, 404, r.do(c[0], c[1], "asha", "").Code, c[0]+" "+c[1])
	}

	assert.Zero(t, r.customer.calls+r.order.calls+r.catalogue.calls+r.inventory.calls+r.account.calls, "none of them reached a backend")
}

func TestStorePublicCallsNeedNoToken(t *testing.T) {
	r := newStoreRig(t)

	w := r.do("GET", "/store/catalogue-svc/product?status=HIDDEN", "", "")
	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "ENABLED", r.catalogue.last.URL.Query().Get("status"), "hidden catalogue entries stay out of listings")

	assert.Equal(t, 200, r.do("GET", "/store/inventory-svc/stock/availability", "", "").Code)
	assert.Zero(t, r.customers.asked, "no token lookup for a public call")
}

func TestStoreRegisterAndLoginArePublicAndRateLimited(t *testing.T) {
	r := newStoreRig(t)

	assert.Equal(t, 200, r.do("POST", "/store/customer-svc/auth/login", "", `{"email":"a@b.co","password":"x"}`).Code)
	assert.Equal(t, 200, r.do("POST", "/store/customer-svc/auth/register", "", `{"name":"A"}`).Code)
	assert.Equal(t, 429, r.do("POST", "/store/customer-svc/auth/login", "", `{}`).Code, "the third attempt in a minute from one address")
}

func TestStoreSignedCallsNeedAShopperToken(t *testing.T) {
	r := newStoreRig(t)

	assert.Equal(t, 401, r.do("GET", "/store/order-svc/order", "", "").Code, "no token")
	assert.Equal(t, 401, r.do("GET", "/store/order-svc/order", "wrong", "").Code)
	assert.Equal(t, 403, r.do("GET", "/store/order-svc/order", "suspended", "").Code, "customer-svc's own refusal is passed on")

	// A team member's token is not a shopper's: it is looked up with customer-svc and refused.
	assert.Equal(t, 401, r.do("GET", "/store/order-svc/order", "admin", "").Code, "an admin token does not work on /store")

	r.customers.down = true
	assert.Equal(t, 503, r.do("GET", "/store/order-svc/order", "asha", "").Code, "authentication down: refused, never let through")
	assert.Zero(t, r.order.calls)
}

func TestStoreSendsTheShoppersIdAndNothingElse(t *testing.T) {
	r := newStoreRig(t)

	w := r.do("GET", "/store/order-svc/order/ABC123", "asha", "",
		"X-Customer-Id", "99", "X-User-Id", "1", "X-All-Access", "true", "X-Seller-Scope", "all", "Cookie", "s=1")

	assert.Equal(t, 200, w.Code)

	got := r.order.last.Header
	assert.Equal(t, "7", got.Get("X-Customer-Id"), "the verified id replaces whatever the client sent")
	assert.Empty(t, got.Get("X-User-Id"), "a shopper is never a team user")
	assert.Empty(t, got.Get("X-All-Access"))
	assert.Empty(t, got.Get("X-Seller-Scope"))
	assert.Empty(t, got.Get("Authorization"), "the token stays at the gateway")
	assert.Empty(t, got.Get("Cookie"))
	assert.Equal(t, "/order/ABC123", r.order.last.URL.Path)
}

func TestStoreForwardsTheTokenOnlyOnCustomerSvcOwnRoutes(t *testing.T) {
	r := newStoreRig(t)

	assert.Equal(t, 200, r.do("GET", "/store/customer-svc/customer/me", "asha", "").Code)
	assert.Equal(t, "Bearer asha", r.customer.last.Header.Get("Authorization"), "customer-svc identifies /customer/me by the token")

	assert.Equal(t, 200, r.do("GET", "/store/order-svc/order", "asha", "").Code)
	assert.Empty(t, r.order.last.Header.Get("Authorization"))
}

func TestStoreHoldsAnOrderListToTheShopper(t *testing.T) {
	r := newStoreRig(t)

	assert.Equal(t, 200, r.do("GET", "/store/order-svc/order?status=PENDING", "asha", "").Code)
	assert.Equal(t, "7", r.order.last.URL.Query().Get("customerId"), "a missing customerId is filled in with their own")
	assert.Equal(t, "PENDING", r.order.last.URL.Query().Get("status"))

	before := r.order.calls

	assert.Equal(t, 403, r.do("GET", "/store/order-svc/order?customerId=8", "asha", "").Code, "someone else's id")
	assert.Equal(t, 403, r.do("GET", "/store/order-svc/order?customerId=7&customerId=8", "asha", "").Code, "a second value cannot slip through")
	assert.Equal(t, 403, r.do("GET", "/store/order-svc/order?customerId=abc", "asha", "").Code)
	assert.Equal(t, before, r.order.calls)
}

func TestStorePlacingAnOrderCannotNameAnotherCustomer(t *testing.T) {
	r := newStoreRig(t)

	assert.Equal(t, 200, r.do("POST", "/store/order-svc/order", "asha", `{"items":[],"customerId":7}`).Code)
	assert.Equal(t, 200, r.do("POST", "/store/order-svc/order", "asha", `{"items":[]}`).Code)

	before := r.order.calls

	assert.Equal(t, 403, r.do("POST", "/store/order-svc/order", "asha", `{"customerId":8}`).Code)
	assert.Equal(t, 403, r.do("POST", "/store/order-svc/order", "asha", `{"CUSTOMERID":8}`).Code, "key case does not matter to the backend")
	assert.Equal(t, 403, r.do("POST", "/store/order-svc/order", "asha", `{"customerId":"8"}`).Code)
	assert.Equal(t, before, r.order.calls)
}

func TestStoreAShopperMayOnlyCancelOrEditTheirDetails(t *testing.T) {
	r := newStoreRig(t)

	for _, body := range []string{
		`{"status":"CANCELLED","cancellationReason":"changed my mind"}`,
		`{"deliveryAddress":{"line1":"x"},"notes":"ring twice"}`,
	} {
		assert.Equal(t, 200, r.do("PATCH", "/store/order-svc/order/ABC", "asha", body).Code, body)
	}

	before := r.order.calls

	for _, body := range []string{
		`{"status":"PACKED"}`,
		`{"STATUS":"COMPLETED"}`,
		`{"status":"CANCELLED","paymentStatus":"PAID"}`,
		`{"orderItems":[{"orderItemId":1,"quantity":0}]}`,
		`{"metaData":{"x":1}}`,
		`{"status":["CANCELLED"]}`,
	} {
		assert.Equal(t, 403, r.do("PATCH", "/store/order-svc/order/ABC", "asha", body).Code, body)
	}

	assert.Equal(t, 415, r.do("PATCH", "/store/order-svc/order/ABC", "asha", "status=PACKED", "Content-Type", "application/x-www-form-urlencoded").Code,
		"a form body cannot be inspected, so it is refused")

	assert.Equal(t, before, r.order.calls, "none of the refused calls reached order-svc")
}

func TestStoreRefusesOversizedBodiesAndOddPaths(t *testing.T) {
	r := newStoreRig(t)

	assert.Equal(t, 413, r.do("PATCH", "/store/order-svc/order/ABC", "asha", `{"notes":"`+strings.Repeat("a", 2000)+`"}`).Code)
	assert.Equal(t, 400, r.do("GET", "/store/order-svc/order/../order-log", "asha", "").Code)
}

func TestStoreDoesNotLetShoppersIntoTheTeamRealm(t *testing.T) {
	r := newStoreRig(t)

	// A shopper token on /api is looked up with account-svc, not customer-svc, and is not valid there.
	assert.Equal(t, 401, r.do("GET", "/api/order-svc/order", "asha", "").Code)
}

func TestStoreHeadersAreStrippedOnTheTeamRealmToo(t *testing.T) {
	r := newStoreRig(t)

	assert.Equal(t, 200, r.do("GET", "/api/catalogue-svc/product", "", "", "X-Customer-Id", "7").Code)
	assert.Empty(t, r.catalogue.last.Header.Get("X-Customer-Id"), "a client cannot pose as a shopper through the team realm either")
}
