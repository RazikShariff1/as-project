package store

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/zopping/gateway-svc/permissions"
)

// A route staff reach through the team realm must never be reachable by a shopper: the allowlist and the staff endpoint
// catalogue stay disjoint, except for the order routes shoppers and staff deliberately share (order-svc tells them apart).
func TestStaffOnlyEndpointsAreNotOnTheShopperAllowlist(t *testing.T) {
	shared := map[string]bool{
		"order-svc POST /order": true, "order-svc GET /order": true, "order-svc GET /order/{refNum}": true,
		"order-svc PATCH /order/{refNum}": true, "order-svc GET /order-log": true,
	}

	for _, e := range permissions.Endpoints() {
		if e.Service != Customers && e.Service != Orders {
			continue
		}

		_, listed := Lookup(e.Service, e.Method, e.Path)
		assert.Equal(t, shared[e.Key()], listed, "%s: a staff route is on the shopper allowlist", e.Key())
	}
}

func TestEveryAllowlistedSignedRouteIsHeldToTheShopper(t *testing.T) {
	for _, r := range routes() {
		e := r.entry
		if e.Class != Signed {
			continue
		}

		switch r.service {
		case Customers:
			assert.True(t, e.ForwardToken, "%s %s: customer-svc must identify the caller by token, never by an id in the call", r.method, r.pattern)
			assert.NotContains(t, r.pattern, "{customerId}")
		case Orders:
			hasRule := e.OwnQuery != "" || e.OwnBody != "" || e.OnlyBody != nil || r.method == "GET"
			assert.True(t, hasRule, "%s %s: a write needs a rule here", r.method, r.pattern)
		}
	}
}
