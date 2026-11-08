package policy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/zopping/gateway-svc/accountclient"
)

const maxBody = 1 << 10

func asSeller(id any) accountclient.Principal {
	return accountclient.Principal{User: accountclient.User{ID: 5, Attributes: map[string]any{"sellerId": id}}}
}

func staff() accountclient.Principal {
	return accountclient.Principal{User: accountclient.User{ID: 3, Attributes: map[string]any{"sellerScope": "all"}}}
}

func admin() accountclient.Principal {
	return accountclient.Principal{User: accountclient.User{ID: 1}, AllAccess: true}
}

func get(target string) *http.Request { return httptest.NewRequest(http.MethodGet, target, nil) }

func jsonReq(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	return r
}

func TestClassify(t *testing.T) {
	tests := []struct {
		service, method, path string
		class                 Class
		force                 map[string]string
	}{
		{"account-svc", "POST", "/auth/login", Public, nil},
		{"catalogue-svc", "GET", "/product", Public, map[string]string{"status": "ENABLED"}},
		{"catalogue-svc", "GET", "/product/42", Public, nil},
		{"catalogue-svc", "GET", "/brand", Public, map[string]string{"status": "ENABLED"}},
		{"inventory-svc", "GET", "/stock/availability", Public, nil},
		{"account-svc", "GET", "/auth/me", AuthOnly, nil},
		{"account-svc", "DELETE", "/auth/session", AuthOnly, nil},

		{"account-svc", "GET", "/auth/check", Internal, nil},
		{"catalogue-svc", "PUT", "/seller-product-request/reconcile", Internal, nil},
		{"inventory-svc", "PUT", "/reservation/release-expired", Internal, nil},
		{"inventory-svc", "PUT", "/stock/1/seller/7/offer", Internal, nil},

		// writing is never public, only reading is
		{"catalogue-svc", "POST", "/product", Protected, nil},
		{"catalogue-svc", "PUT", "/product/42", Protected, nil},
		{"catalogue-svc", "DELETE", "/product/42", Protected, nil},
		{"catalogue-svc", "GET", "/seller-product-request", Protected, nil},
		{"inventory-svc", "GET", "/stock", Protected, nil},
		{"inventory-svc", "GET", "/stock/1/seller/7", Protected, nil},
		{"account-svc", "GET", "/user", Protected, nil},
		{"account-svc", "POST", "/auth/me", Protected, nil},
		{"anything-svc", "GET", "/x", Protected, nil},
	}

	for _, tc := range tests {
		class, force := Classify(tc.service, tc.method, tc.path)
		assert.Equal(t, tc.class, class, "%s %s %s", tc.method, tc.service, tc.path)
		assert.Equal(t, tc.force, force, "%s %s %s", tc.method, tc.service, tc.path)
	}
}

func TestSellerPathScope(t *testing.T) {
	for _, path := range []string{
		"/stock/1/seller/7", "/stock/1/seller/7/price", "/stock/1/seller/7/adjust", "/stock/1/seller/7/movement",
		"/stock/1/seller/7/price-log",
	} {
		_, err := Enforce(asSeller(7.0), "inventory-svc", "PUT", path, get(path), maxBody)
		assert.NoError(t, err, "own seller: %s", path)

		_, err = Enforce(asSeller(7.0), "inventory-svc", "PUT", strings.Replace(path, "seller/7", "seller/8", 1), get(path), maxBody)
		assert.ErrorAs(t, err, new(*Denied), "another seller: %s", path)
	}
}

func TestSellerPathScope_Variants(t *testing.T) {
	_, err := Enforce(asSeller(7.0), "inventory-svc", "GET", "/stock/1/seller/007", get("/x"), maxBody)
	assert.NoError(t, err, "a leading zero is the same number to the backend")

	for _, bad := range []string{"/stock/1/seller/abc", "/stock/1/seller/7x", "/stock/1/seller/-7", "/stock/1/seller/7.0"} {
		_, err = Enforce(asSeller(7.0), "inventory-svc", "GET", bad, get("/x"), maxBody)
		assert.ErrorAs(t, err, new(*Denied), bad)
	}

	_, err = Enforce(admin(), "inventory-svc", "PUT", "/stock/1/seller/8/price", get("/x"), maxBody)
	assert.NoError(t, err, "admins act for any seller")
}

func TestStaffWithASellerScopeActAcrossSellersButNotAcrossAccounts(t *testing.T) {
	_, err := Enforce(staff(), "inventory-svc", "PUT", "/stock/1/seller/8/price", get("/x"), maxBody)
	assert.NoError(t, err, "a catalogue manager or support agent may work on any seller")

	q, err := Enforce(staff(), "catalogue-svc", "GET", "/seller-product-request", get("/seller-product-request"), maxBody)
	assert.NoError(t, err)
	assert.Empty(t, q.Get("sellerId"), "their list is not narrowed to one seller")

	_, err = Enforce(staff(), "catalogue-svc", "POST", "/seller-product-request", jsonReq("POST", "/x", `{"sellerId": 99}`), maxBody)
	assert.NoError(t, err)

	_, err = Enforce(staff(), "account-svc", "PUT", "/user/6/password", get("/x"), maxBody)
	assert.ErrorAs(t, err, new(*Denied), "but a sellerScope does not let them change other people's accounts")

	// a seller who is also (wrongly) given an unrecognised scope is still limited
	p := asSeller(7.0)
	p.User.Attributes["sellerScope"] = "yes please"

	_, err = Enforce(p, "inventory-svc", "PUT", "/stock/1/seller/8/price", get("/x"), maxBody)
	assert.ErrorAs(t, err, new(*Denied))
}

func TestAccountNotLinkedToASellerIsDeniedNotLetThrough(t *testing.T) {
	for _, p := range []accountclient.Principal{
		{User: accountclient.User{ID: 5}},
		asSeller("7"), // a string, not a number
		asSeller(0.0), // not a seller id
		asSeller(7.5), // not a whole number
		asSeller(-3.0),
		asSeller(nil),
	} {
		_, err := Enforce(p, "inventory-svc", "GET", "/stock/1/seller/7", get("/x"), maxBody)

		var d *Denied

		assert.ErrorAs(t, err, &d)
		assert.Equal(t, "this account is not linked to a seller", d.Reason, "%v", p.User.Attributes)
	}
}

func TestSellerQueryScopeFillsInTheCallersOwnId(t *testing.T) {
	q, err := Enforce(asSeller(7.0), "inventory-svc", "GET", "/stock", get("/stock?status=ENABLED"), maxBody)
	assert.NoError(t, err)
	assert.Equal(t, "7", q.Get("sellerId"), "no sellerId asked for means everyone's, so it is limited to the caller's own")
	assert.Equal(t, "ENABLED", q.Get("status"), "other filters are kept")

	q, err = Enforce(asSeller(7.0), "inventory-svc", "GET", "/stock", get("/stock?sellerId=7"), maxBody)
	assert.NoError(t, err)
	assert.Equal(t, []string{"7"}, q["sellerId"])

	for _, target := range []string{"/stock?sellerId=8", "/stock?sellerId=7&sellerId=8", "/stock?sellerId=", "/stock?sellerId=abc"} {
		_, err = Enforce(asSeller(7.0), "inventory-svc", "GET", "/stock", get(target), maxBody)
		assert.ErrorAs(t, err, new(*Denied), target)
	}

	q, err = Enforce(admin(), "inventory-svc", "GET", "/stock", get("/stock"), maxBody)
	assert.NoError(t, err)
	assert.Empty(t, q.Get("sellerId"), "admins see every seller")

	for _, route := range [][2]string{{"catalogue-svc", "/seller-product-request"}, {"catalogue-svc", "/permissible-seller"}} {
		q, err = Enforce(asSeller(7.0), route[0], "GET", route[1], get(route[1]), maxBody)
		assert.NoError(t, err, route[1])
		assert.Equal(t, "7", q.Get("sellerId"), route[1])
	}
}

func TestSellerBodyScope(t *testing.T) {
	ok := []string{`{"sellerId": 7, "productId": 3}`, `{"productId": 3}`, `{"sellerId": "7"}`, `{}`}
	for _, body := range ok {
		_, err := Enforce(asSeller(7.0), "catalogue-svc", "POST", "/seller-product-request", jsonReq("POST", "/x", body), maxBody)
		assert.NoError(t, err, body)
	}

	bad := []string{
		`{"sellerId": 8}`, `{"sellerId": "8"}`, `{"sellerId": null}`, `{"sellerId": 7.5}`, `{"sellerId": [7]}`, `{"sellerId": true}`,
		// Go's JSON decoder fills a struct field from a key of any letter case, so these all reach the backend's SellerID
		`{"SELLERID": 8}`, `{"sellerid": 8}`, `{"SellerId": 8}`, `{"sellerId": 7, "SELLERID": 8}`,
		// duplicate keys: the backend keeps the last one, and so must the check, whichever is read
		`{"sellerId": 7, "sellerId": 8}`, `{"sellerId": 8, "sellerId": 7, "SELLERID": 9}`,
	}

	for _, body := range bad {
		_, err := Enforce(asSeller(7.0), "catalogue-svc", "POST", "/seller-product-request", jsonReq("POST", "/x", body), maxBody)
		assert.ErrorAs(t, err, new(*Denied), body)
	}

	_, err := Enforce(admin(), "catalogue-svc", "POST", "/seller-product-request", jsonReq("POST", "/x", `{"sellerId": 99}`), maxBody)
	assert.NoError(t, err, "admins file requests for any seller")
}

func TestBodyScopeOnlyAcceptsJSON(t *testing.T) {
	// GoFr also binds form and multipart bodies, which this check cannot read, so a form body must not slip past it.
	for name, ct := range map[string]string{
		"form":      "application/x-www-form-urlencoded",
		"multipart": "multipart/form-data; boundary=x",
		"text":      "text/plain",
		"none":      "",
		"garbage":   "not a media type",
	} {
		r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("sellerId=99"))
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}

		_, err := Enforce(asSeller(7.0), "catalogue-svc", "POST", "/seller-product-request", r, maxBody)
		assert.ErrorAs(t, err, new(*UnsupportedMedia), name)
	}

	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"sellerId":7}`))
	r.Header.Set("Content-Type", "application/json; charset=utf-8")

	_, err := Enforce(asSeller(7.0), "catalogue-svc", "POST", "/seller-product-request", r, maxBody)
	assert.NoError(t, err, "JSON with a charset is still JSON")

	_, err = Enforce(asSeller(7.0), "catalogue-svc", "POST", "/seller-product-request",
		httptest.NewRequest(http.MethodPost, "/x", http.NoBody), maxBody)
	assert.NoError(t, err, "an empty body has nothing to check; the backend rejects it itself")
}

func TestBodyIsRestoredForTheBackendAndLimited(t *testing.T) {
	r := jsonReq("POST", "/x", `{"sellerId": 7, "productId": 3}`)

	_, err := Enforce(asSeller(7.0), "catalogue-svc", "POST", "/seller-product-request", r, maxBody)
	assert.NoError(t, err)

	forwarded, _ := io.ReadAll(r.Body)
	assert.JSONEq(t, `{"sellerId": 7, "productId": 3}`, string(forwarded), "the backend still gets the whole body")
	assert.Equal(t, int64(len(forwarded)), r.ContentLength)

	big := jsonReq("POST", "/x", `{"sellerId": 7, "pad": "`+strings.Repeat("x", 2*maxBody)+`"}`)

	_, err = Enforce(asSeller(7.0), "catalogue-svc", "POST", "/seller-product-request", big, maxBody)
	assert.ErrorIs(t, err, TooLarge)

	_, err = Enforce(asSeller(7.0), "catalogue-svc", "POST", "/seller-product-request", jsonReq("POST", "/x", `[1,2]`), maxBody)
	assert.NoError(t, err, "not an object: the backend answers 400 for it too")
}

func TestUserScope(t *testing.T) {
	me := accountclient.Principal{User: accountclient.User{ID: 5}}

	for _, path := range []string{"/user/5", "/user/5/password", "/user/5/session"} {
		_, err := Enforce(me, "account-svc", "PUT", path, get("/x"), maxBody)
		assert.NoError(t, err, "own account: %s", path)

		_, err = Enforce(me, "account-svc", "PUT", strings.Replace(path, "/5", "/6", 1), get("/x"), maxBody)
		assert.ErrorAs(t, err, new(*Denied), "someone else's: %s", path)
	}

	_, err := Enforce(admin(), "account-svc", "PUT", "/user/6/password", get("/x"), maxBody)
	assert.NoError(t, err, "admins manage any account")

	_, err = Enforce(me, "account-svc", "PUT", "/user/6/password/reset", get("/x"), maxBody)
	assert.NoError(t, err, "the reset route has no scope rule here: it is admin-only by role")
}

func TestUnrelatedRoutesAreUntouched(t *testing.T) {
	for _, tc := range [][3]string{
		{"catalogue-svc", "GET", "/product"}, {"catalogue-svc", "PUT", "/product/42"}, {"inventory-svc", "GET", "/stock/availability"},
		{"inventory-svc", "POST", "/reservation"}, {"account-svc", "GET", "/role"},
	} {
		q, err := Enforce(asSeller(7.0), tc[0], tc[1], tc[2], get("/x?a=b"), maxBody)
		assert.NoError(t, err, tc)
		assert.Equal(t, url.Values{"a": {"b"}}, q, tc)
	}
}
