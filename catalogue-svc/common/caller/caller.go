// Package caller carries the identity the gateway forwards (X-Seller-Id, X-Seller-Scope) from an HTTP middleware into the
// handler's context, since GoFr handlers cannot read headers.
//
// The headers are trusted: this service has no auth of its own. That is safe only because the gateway strips any such headers
// a client sends and is the only way to reach this service, so it must never be exposed directly.
package caller

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"gofr.dev/pkg/gofr"
)

type key int

const (
	sellerKey key = iota
	allSellersKey
)

const (
	headerSeller = "X-Seller-Id"
	// headerScope is "all" when the gateway says the caller may act across sellers (admins, and staff given that scope).
	headerScope = "X-Seller-Scope"
)

// Middleware stores the forwarded seller id and seller scope in the request context.
func Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			if id, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get(headerSeller)), 10, 64); err == nil && id > 0 {
				ctx = context.WithValue(ctx, sellerKey, id)
			}

			if strings.EqualFold(strings.TrimSpace(r.Header.Get(headerScope)), "all") {
				ctx = context.WithValue(ctx, allSellersKey, true)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Seller returns the seller the caller is restricted to. ok is false for internal calls and for callers the gateway says may act
// across sellers (X-Seller-Scope: all), which are not restricted to one seller.
func Seller(ctx *gofr.Context) (id int64, ok bool) {
	if ctx == nil || ctx.Request == nil {
		return 0, false
	}

	if all, _ := ctx.Request.Context().Value(allSellersKey).(bool); all {
		return 0, false
	}

	id, ok = ctx.Request.Context().Value(sellerKey).(int64)

	return id, ok
}
