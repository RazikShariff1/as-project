// Package authctx carries request metadata GoFr handlers cannot read directly (the bearer token, client address and
// user agent) from an HTTP middleware into the handler's context.
package authctx

import (
	"net"
	"net/http"
	"strings"

	"gofr.dev/pkg/gofr"
)

type key int

const (
	tokenKey key = iota
	userAgentKey
	ipKey
)

const bearerPrefix = "bearer "

// Middleware stores the bearer token, user agent and client IP in the request context.
func Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			if h := strings.TrimSpace(r.Header.Get("Authorization")); len(h) > len(bearerPrefix) &&
				strings.EqualFold(h[:len(bearerPrefix)], bearerPrefix) {
				ctx = withValue(ctx, tokenKey, strings.TrimSpace(h[len(bearerPrefix):]))
			}

			ctx = withValue(ctx, userAgentKey, r.UserAgent())
			ctx = withValue(ctx, ipKey, clientIP(r))

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// clientIP prefers the first X-Forwarded-For hop (the gateway sets it) over the socket address.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.TrimSpace(strings.Split(fwd, ",")[0])
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

// Token returns the bearer token of the current request, or "" when there is none.
func Token(ctx *gofr.Context) string { return value(ctx, tokenKey) }

// UserAgent returns the current request's user agent.
func UserAgent(ctx *gofr.Context) string { return value(ctx, userAgentKey) }

// IP returns the current request's client address.
func IP(ctx *gofr.Context) string { return value(ctx, ipKey) }
