package permissions

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/zopping/gateway-svc/policy"
	"github.com/zopping/gateway-svc/routing"
)

// path turns a pattern such as /stock/{id}/seller/{sellerId} into a concrete path the policy can classify.
func concrete(pattern string) string {
	parts := strings.Split(pattern, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, "{") {
			parts[i] = "1"
		}
	}

	return strings.Join(parts, "/")
}

func TestEveryEndpointIsUniqueAndWellFormed(t *testing.T) {
	seen := map[string]bool{}

	for _, e := range Endpoints() {
		assert.False(t, seen[e.Key()], "duplicate %s", e.Key())
		seen[e.Key()] = true

		assert.NotEmpty(t, e.Description, e.Key())
		assert.True(t, strings.HasPrefix(e.Path, "/"), e.Key())
		assert.Contains(t, []string{"GET", "POST", "PUT", "PATCH", "DELETE"}, e.Method, e.Key())
		assert.Contains(t, []string{"account-svc", "catalogue-svc", "inventory-svc", "order-svc"}, e.Service, e.Key())
	}
}

// A role grant only matters for routes the gateway sends through the permission check. Listing a public, sign-in-only or internal
// route here would be dead weight at best, and a hole in the policy at worst.
func TestOnlyProtectedRoutesAreInTheCatalogue(t *testing.T) {
	for _, e := range Endpoints() {
		class, _ := policy.Classify(e.Service, e.Method, concrete(e.Path))
		assert.Equal(t, policy.Protected, class, "%s is %v in the gateway policy, so it needs no role", e.Key(), class)
	}
}

func TestEveryGrantNamesACatalogueEndpoint(t *testing.T) {
	known := map[string]bool{}
	for _, e := range Endpoints() {
		known[e.Key()] = true
	}

	for _, r := range Roles() {
		for _, g := range r.Grants {
			assert.True(t, known[g], "role %s grants %q, which is not in the catalogue", r.Name, g)
		}
	}
}

func TestRolesAreWellFormed(t *testing.T) {
	names := map[string]bool{}

	for _, r := range Roles() {
		assert.False(t, names[r.Name], "duplicate role %s", r.Name)
		names[r.Name] = true

		seen := map[string]bool{}
		for _, g := range r.Grants {
			assert.False(t, seen[g], "role %s grants %s twice", r.Name, g)
			seen[g] = true
		}

		if r.AllAccess {
			assert.Empty(t, r.Grants, "%s has all access, so listing endpoints is pointless", r.Name)
		}
	}

	assert.True(t, names["admin"] && names["seller"] && names["catalogue-manager"] && names["customer"] && names["order-manager"])
}

func grants(name string) map[string]bool {
	for _, r := range Roles() {
		if r.Name == name {
			m := map[string]bool{}
			for _, g := range r.Grants {
				m[g] = true
			}

			return m
		}
	}

	return nil
}

func TestASellerCannotApproveCurateOrManageOthers(t *testing.T) {
	seller := grants("seller")

	for _, forbidden := range []string{
		Key("catalogue-svc", "PUT", "/seller-product-request/{id}/approval"), // a seller must never approve their own request
		Key("catalogue-svc", "PUT", "/product/{id}/permissible-seller"),      // nor decide who may sell
		Key("catalogue-svc", "POST", "/product"), Key("catalogue-svc", "PUT", "/product/{id}"), Key("catalogue-svc", "POST", "/variant"),
		Key("catalogue-svc", "POST", "/seller"), Key("catalogue-svc", "PUT", "/seller/{id}"),
		Key("inventory-svc", "POST", "/reservation"), Key("inventory-svc", "PUT", "/reservation/{orderRef}/confirm"),
		Key("account-svc", "GET", "/user"), Key("account-svc", "POST", "/user"), Key("account-svc", "PUT", "/user/{id}"),
		Key("account-svc", "PUT", "/user/{id}/password/reset"), Key("account-svc", "POST", "/role"), Key("account-svc", "POST", "/endpoint"),
	} {
		assert.False(t, seller[forbidden], "a seller must not be granted %s", forbidden)
	}

	for _, needed := range []string{
		Key("catalogue-svc", "POST", "/seller-product-request"), Key("inventory-svc", "PUT", "/stock/{id}/seller/{sellerId}/adjust"),
		Key("inventory-svc", "PUT", "/stock/{id}/seller/{sellerId}/price"), Key("account-svc", "PUT", "/user/{id}/password"),
	} {
		assert.True(t, seller[needed], "a seller needs %s", needed)
	}
}

func TestEverySellerGrantIsScopedToTheirOwnData(t *testing.T) {
	// Each inventory route a seller can call must be one the gateway scopes to the caller's own seller id, otherwise a seller
	// could reach other sellers' stock. policy.Enforce is exercised in the policy tests; this guards the catalogue against
	// granting a seller an unscoped route by mistake.
	for g := range grants("seller") {
		if !strings.HasPrefix(g, "inventory-svc ") {
			continue
		}

		_, path, _ := strings.Cut(strings.TrimPrefix(g, "inventory-svc "), " ")
		_, scoped := routing.Match("/stock/{id}/seller/{sellerId}/**", concrete(path))
		_, scopedExact := routing.Match("/stock/{id}/seller/{sellerId}", concrete(path))

		assert.True(t, scoped || scopedExact || path == "/stock", "seller grant %s is not under /stock/{id}/seller/{sellerId} or the scoped list", g)
	}
}

func TestCatalogueManagerHasNoInventoryOrAccountAccess(t *testing.T) {
	for g := range grants("catalogue-manager") {
		assert.True(t, strings.HasPrefix(g, "catalogue-svc "), "catalogue-manager should only touch the catalogue, got %s", g)
	}

	assert.True(t, grants("catalogue-manager")[Key("catalogue-svc", "PUT", "/seller-product-request/{id}/approval")])
}

func TestACustomerCanOrderButNotRunOrders(t *testing.T) {
	g := grants("customer")

	for _, r := range [][2]string{{"POST", "/order"}, {"GET", "/order"}, {"GET", "/order/{id}"}, {"GET", "/order/{id}/log"}, {"PUT", "/order/{id}/cancel"}} {
		assert.True(t, g[Key("order-svc", r[0], r[1])], "%v", r)
	}

	assert.False(t, g[Key("order-svc", "PUT", "/order/{id}/status")], "only staff move an order through its statuses")
	assert.False(t, g[Key("order-svc", "PUT", "/order/{id}/payment-status")], "only staff and payment-svc record payment")

	for k := range g {
		assert.True(t, strings.HasPrefix(k, "order-svc "), "a customer should only touch orders, got %s", k)
	}
}

func TestAnOrderManagerCanRunOrders(t *testing.T) {
	g := grants("order-manager")

	assert.True(t, g[Key("order-svc", "PUT", "/order/{id}/status")])
	assert.True(t, g[Key("order-svc", "PUT", "/order/{id}/payment-status")])
	assert.True(t, g[Key("order-svc", "GET", "/order")])
}
