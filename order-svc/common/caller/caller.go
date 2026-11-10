// Package caller carries the identity the gateway forwards (X-Customer-Id, X-User-Id, X-All-Access, X-Seller-Scope) from an HTTP
// middleware into the handler's context, since GoFr handlers cannot read headers.
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
	userKey key = iota
	staffKey
	customerKey
)

const (
	// headerCustomer is set by the gateway for a signed-in shopper (customer-svc's id space). It is separate from X-User-Id,
	// which is a team member's account id: the two are different people and their ids can be equal.
	headerCustomer  = "X-Customer-Id"
	headerUser      = "X-User-Id"
	headerAllAccess = "X-All-Access"
	headerScope     = "X-Seller-Scope"
)

// Middleware stores the forwarded user id, and whether the gateway says the caller is staff, in the request context.
func Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			if id, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get(headerCustomer)), 10, 64); err == nil && id > 0 {
				ctx = context.WithValue(ctx, customerKey, id)
			}

			if id, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get(headerUser)), 10, 64); err == nil && id > 0 {
				ctx = context.WithValue(ctx, userKey, id)
			}

			if strings.EqualFold(strings.TrimSpace(r.Header.Get(headerAllAccess)), "true") ||
				strings.EqualFold(strings.TrimSpace(r.Header.Get(headerScope)), "all") {
				ctx = context.WithValue(ctx, staffKey, true)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// User returns the signed-in caller the request is from, for recording who made a change; ok is false for internal calls (no
// identity forwarded). A shopper's customer id is returned when there is one.
func User(ctx *gofr.Context) (id int64, ok bool) {
	if ctx == nil || ctx.Request == nil {
		return 0, false
	}

	if id, ok = ctx.Request.Context().Value(customerKey).(int64); ok {
		return id, true
	}

	id, ok = ctx.Request.Context().Value(userKey).(int64)

	return id, ok
}

// Customer returns the customer id a request is restricted to: a shopper (X-Customer-Id) may only touch their own orders, whatever
// else the request claims. A signed-in team member who is not staff is restricted by their user id; ok is false for staff and
// internal calls, which are not restricted.
func Customer(ctx *gofr.Context) (id int64, ok bool) {
	if ctx == nil || ctx.Request == nil {
		return 0, false
	}

	if id, ok = ctx.Request.Context().Value(customerKey).(int64); ok {
		return id, true
	}

	if staff, _ := ctx.Request.Context().Value(staffKey).(bool); staff {
		return 0, false
	}

	return User(ctx)
}
