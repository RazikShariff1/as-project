// Package policy decides how the gateway treats a call: whether it needs a token, whether it is reachable from outside at all,
// and which part of a request must match the caller.
package policy

import "github.com/zopping/gateway-svc/routing"

// Class is how a route is treated.
type Class int

const (
	// Protected routes need a valid token and a role that grants the endpoint. Everything not listed below is Protected.
	Protected Class = iota
	// Public routes need no token.
	Public
	// AuthOnly routes need a valid token but no endpoint permission: calls every signed-in user may make about themselves.
	AuthOnly
	// Internal routes are for other services and schedulers and are not reachable through the gateway at all.
	Internal
)

type route struct {
	service, method, pattern string
	class                    Class
	// forceQuery sets these query parameters on the way through, whatever the client sent. It keeps hidden catalogue
	// entries out of anonymous listings.
	forceQuery map[string]string
}

func routes() []route {
	const (
		account, catalogue, inventory = "account-svc", "catalogue-svc", "inventory-svc"
		get                           = "GET"
	)

	enabled := map[string]string{"status": "ENABLED"}

	return []route{
		// Not for outside callers: the gateway makes the auth check itself; the others are for services and schedulers.
		{account, get, "/auth/check", Internal, nil},
		{catalogue, "PUT", "/seller-product-request/reconcile", Internal, nil},
		{inventory, "PUT", "/reservation/release-expired", Internal, nil},
		{inventory, "PUT", "/stock/{id}/seller/{sellerId}/offer", Internal, nil},

		// Browsing the storefront and logging in need no token. Listings are limited to enabled entries.
		{account, "POST", "/auth/login", Public, nil},
		{catalogue, get, "/product", Public, enabled},
		{catalogue, get, "/product/{id}", Public, nil},
		{catalogue, get, "/variant", Public, enabled},
		{catalogue, get, "/variant/{id}", Public, nil},
		{catalogue, get, "/brand", Public, enabled},
		{catalogue, get, "/brand/{id}", Public, nil},
		{catalogue, get, "/category", Public, enabled},
		{catalogue, get, "/category/{id}", Public, nil},
		{inventory, get, "/stock/availability", Public, nil},

		// Any signed-in user may ask who they are and sign out.
		{account, get, "/auth/me", AuthOnly, nil},
		{account, "DELETE", "/auth/session", AuthOnly, nil},
	}
}

// Classify returns how a call is treated, and for Public routes the query parameters to force.
func Classify(service, method, path string) (Class, map[string]string) {
	for _, r := range routes() {
		if r.service != service || r.method != method {
			continue
		}

		if _, ok := routing.Match(r.pattern, path); ok {
			return r.class, r.forceQuery
		}
	}

	return Protected, nil
}
