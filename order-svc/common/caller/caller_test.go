package caller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"
)

// run passes a request with the given headers through the middleware and returns the context a handler would get.
func run(headers map[string]string) *gofr.Context {
	r := httptest.NewRequest(http.MethodGet, "/order", nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}

	var ctx *gofr.Context

	Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		ctx = &gofr.Context{Request: gofrhttp.NewRequest(req)}
	})).ServeHTTP(httptest.NewRecorder(), r)

	return ctx
}

func TestAShopperIsRestrictedToTheirCustomerId(t *testing.T) {
	ctx := run(map[string]string{"X-Customer-Id": "7"})

	id, restricted := Customer(ctx)
	assert.True(t, restricted)
	assert.Equal(t, int64(7), id)

	who, ok := User(ctx)
	assert.True(t, ok)
	assert.Equal(t, int64(7), who, "changes are recorded against the shopper")
}

func TestAShopperStaysRestrictedEvenIfStaffHeadersAreAlsoPresent(t *testing.T) {
	ctx := run(map[string]string{"X-Customer-Id": "7", "X-All-Access": "true", "X-Seller-Scope": "all", "X-User-Id": "1"})

	id, restricted := Customer(ctx)
	assert.True(t, restricted, "the customer header wins: staff headers cannot widen a shopper's reach")
	assert.Equal(t, int64(7), id)
}

func TestStaffAreNotRestrictedAndAreIdentifiedByUserId(t *testing.T) {
	ctx := run(map[string]string{"X-User-Id": "3", "X-Seller-Scope": "all"})

	_, restricted := Customer(ctx)
	assert.False(t, restricted)

	who, ok := User(ctx)
	assert.True(t, ok)
	assert.Equal(t, int64(3), who)
}

func TestAnInternalCallHasNoIdentity(t *testing.T) {
	ctx := run(nil)

	_, restricted := Customer(ctx)
	assert.False(t, restricted)

	_, ok := User(ctx)
	assert.False(t, ok)
}

func TestMalformedCustomerIdsAreIgnored(t *testing.T) {
	for _, v := range []string{"abc", "0", "-4", ""} {
		_, restricted := Customer(run(map[string]string{"X-Customer-Id": v}))
		assert.False(t, restricted, v)
	}
}
