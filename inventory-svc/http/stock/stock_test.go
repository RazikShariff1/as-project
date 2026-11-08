package stock

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/inventory-svc/common/apperr"
	"github.com/zopping/inventory-svc/common/caller"
)

// ctxFor builds the context a handler gets for /stock/{id}/seller/{sellerId}, with the identity headers the gateway forwards.
func ctxFor(t *testing.T, vars map[string]string, headers map[string]string) *gofr.Context {
	t.Helper()

	r := httptest.NewRequest(http.MethodPut, "/stock/1/seller/7/price", nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}

	r = mux.SetURLVars(r, vars)

	var ctx *gofr.Context

	caller.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		ctx = &gofr.Context{Request: gofrhttp.NewRequest(req)}
	})).ServeHTTP(httptest.NewRecorder(), r)

	return ctx
}

func TestIdsReadsVariantAndSeller(t *testing.T) {
	v, s, err := ids(ctxFor(t, map[string]string{"id": "1", "sellerId": "7"}, nil))
	assert.NoError(t, err)
	assert.Equal(t, int64(1), v)
	assert.Equal(t, int64(7), s)

	for _, vars := range []map[string]string{{"id": "x", "sellerId": "7"}, {"id": "1", "sellerId": "0"}, {"id": "0", "sellerId": "7"}} {
		_, _, err := ids(ctxFor(t, vars, nil))
		assert.Error(t, err, "%v", vars)
	}
}

func TestASellerCanOnlyNameTheirOwnSeller(t *testing.T) {
	_, s, err := ids(ctxFor(t, map[string]string{"id": "1", "sellerId": "7"}, map[string]string{"X-Seller-Id": "7"}))
	assert.NoError(t, err)
	assert.Equal(t, int64(7), s)

	var forbidden *apperr.Forbidden

	_, _, err = ids(ctxFor(t, map[string]string{"id": "1", "sellerId": "8"}, map[string]string{"X-Seller-Id": "7"}))
	assert.ErrorAs(t, err, &forbidden, "the backstop behind the gateway's own check")

	_, _, err = ids(ctxFor(t, map[string]string{"id": "1", "sellerId": "8"}, map[string]string{"X-Seller-Id": "7", "X-Seller-Scope": "all"}))
	assert.NoError(t, err, "an admin acts for any seller")

	_, _, err = ids(ctxFor(t, map[string]string{"id": "1", "sellerId": "8"}, nil))
	assert.NoError(t, err, "an internal call, such as catalogue-svc syncing an offer, is not restricted")
}
