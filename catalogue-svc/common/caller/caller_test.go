package caller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"
)

func through(t *testing.T, headers map[string]string) (int64, bool) {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}

	var (
		id int64
		ok bool
	)

	Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		id, ok = Seller(&gofr.Context{Request: gofrhttp.NewRequest(req)})
	})).ServeHTTP(httptest.NewRecorder(), r)

	return id, ok
}

func TestSeller(t *testing.T) {
	id, ok := through(t, map[string]string{"X-Seller-Id": "7"})
	assert.True(t, ok)
	assert.Equal(t, int64(7), id)

	_, ok = through(t, nil)
	assert.False(t, ok, "an internal call, such as the offer sync from the subscriber, carries no seller and is not restricted")

	for _, bad := range []string{"", "abc", "0", "-3", "7.5", "7 7"} {
		_, ok = through(t, map[string]string{"X-Seller-Id": bad})
		assert.False(t, ok, "%q is not a seller id", bad)
	}
}

func TestCallersWithAnAllSellersScopeAreNotRestrictedToOneSeller(t *testing.T) {
	_, ok := through(t, map[string]string{"X-Seller-Id": "7", "X-Seller-Scope": "all"})
	assert.False(t, ok, "an admin, or staff with a seller scope, may act for any seller")

	_, ok = through(t, map[string]string{"X-Seller-Id": "7", "X-Seller-Scope": "ALL"})
	assert.False(t, ok, "the value is not case-sensitive")

	for _, scope := range []string{"", "none", "true", "7", "all sellers"} {
		_, ok = through(t, map[string]string{"X-Seller-Id": "7", "X-Seller-Scope": scope})
		assert.True(t, ok, "%q does not lift the restriction", scope)
	}

	_, ok = through(t, map[string]string{"X-Seller-Id": "7", "X-All-Access": "true"})
	assert.True(t, ok, "the admin flag alone no longer decides this: the gateway sends X-Seller-Scope for admins")
}

func TestSellerWithoutARequest(t *testing.T) {
	_, ok := Seller(nil)
	assert.False(t, ok)

	_, ok = Seller(&gofr.Context{})
	assert.False(t, ok, "a context with no request, as in unit tests, is unrestricted")
}
