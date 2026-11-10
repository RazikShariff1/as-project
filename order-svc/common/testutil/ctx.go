// Package testutil builds the request contexts the services and handlers get, for tests.
package testutil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"
	"gofr.dev/pkg/gofr/logging"

	"github.com/zopping/order-svc/common/caller"
)

// Identities as the gateway forwards them.
var (
	Customer = map[string]string{"X-User-Id": "5"}
	Staff    = map[string]string{"X-User-Id": "1", "X-All-Access": "true"}
)

// CtxAs builds the context a service gets for a request carrying the given gateway identity headers.
func CtxAs(t *testing.T, headers map[string]string) *gofr.Context {
	t.Helper()

	return CtxWithQuery(t, headers, "")
}

// CtxWithQuery is CtxAs for a GET /order with the given raw query string.
func CtxWithQuery(t *testing.T, headers map[string]string, query string) *gofr.Context {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, "/order?"+query, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}

	var ctx *gofr.Context

	caller.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		ctx = &gofr.Context{
			Request:       gofrhttp.NewRequest(req),
			ContextLogger: logging.ContextLoggerFor(context.Background(), logging.NewMockLogger(logging.DEBUG)),
		}
	})).ServeHTTP(httptest.NewRecorder(), r)

	return ctx
}
