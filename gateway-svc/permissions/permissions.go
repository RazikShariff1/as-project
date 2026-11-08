// Package permissions is the platform's catalogue of protected endpoints and the roles that may call them. It is applied to
// account-svc by cmd/seed. Only Protected routes appear here: public, sign-in-only and internal routes are handled by the
// gateway's route policy (package policy) and need no role.
package permissions

// Endpoint is one route of one service.
type Endpoint struct {
	Service, Method, Path, Description string
}

// Key identifies an endpoint in a role's grants.
func (e Endpoint) Key() string { return Key(e.Service, e.Method, e.Path) }

// Key builds the identifier of an endpoint.
func Key(service, method, path string) string { return service + " " + method + " " + path }

// Role is a named set of endpoints. A role with AllAccess may call everything, so it lists none.
type Role struct {
	Name, Description string
	AllAccess         bool
	Grants            []string // endpoint keys
}

const (
	catalogue = "catalogue-svc"
	inventory = "inventory-svc"
	account   = "account-svc"
)

func ep(service, method, path, description string) Endpoint {
	return Endpoint{Service: service, Method: method, Path: path, Description: description}
}

// Endpoints lists every protected route the platform exposes through the gateway.
func Endpoints() []Endpoint {
	const stock = "/stock/{id}/seller/{sellerId}"

	return []Endpoint{
		// catalogue-svc: sellers, then content writes (reads of content are public)
		ep(catalogue, "GET", "/seller", "List sellers"),
		ep(catalogue, "POST", "/seller", "Create a seller"),
		ep(catalogue, "GET", "/seller/{id}", "Get a seller"),
		ep(catalogue, "PUT", "/seller/{id}", "Update a seller"),
		ep(catalogue, "POST", "/brand", "Create a brand"),
		ep(catalogue, "PUT", "/brand/{id}", "Update a brand"),
		ep(catalogue, "DELETE", "/brand/{id}", "Delete a brand"),
		ep(catalogue, "POST", "/category", "Create a category"),
		ep(catalogue, "PUT", "/category/{id}", "Update a category"),
		ep(catalogue, "DELETE", "/category/{id}", "Delete a category"),
		ep(catalogue, "POST", "/product", "Create a product"),
		ep(catalogue, "PUT", "/product/{id}", "Update a product"),
		ep(catalogue, "DELETE", "/product/{id}", "Delete a product"),
		ep(catalogue, "POST", "/variant", "Create a variant"),
		ep(catalogue, "PUT", "/variant/{id}", "Update a variant"),
		ep(catalogue, "DELETE", "/variant/{id}", "Delete a variant"),
		ep(catalogue, "PUT", "/product/{id}/permissible-seller", "Set which sellers may sell a product"),
		ep(catalogue, "GET", "/permissible-seller", "List which sellers may sell which products"),
		ep(catalogue, "GET", "/seller-product-request", "List seller product requests"),
		ep(catalogue, "POST", "/seller-product-request", "File a request to sell a product"),
		ep(catalogue, "GET", "/seller-product-request/{id}", "Get a seller product request"),
		ep(catalogue, "PUT", "/seller-product-request/{id}", "Revise and resubmit a seller product request"),
		ep(catalogue, "DELETE", "/seller-product-request/{id}", "Delete a seller product request"),
		ep(catalogue, "PUT", "/seller-product-request/{id}/approval", "Approve or reject a seller product request"),
		ep(catalogue, "PUT", "/seller-product-request/{id}/visibility", "Show or hide a live offer"),

		// inventory-svc: a seller's offers live under their own id
		ep(inventory, "GET", "/stock", "List offers"),
		ep(inventory, "GET", stock, "Get a seller's offer for a variant"),
		ep(inventory, "PUT", stock, "Set an offer's stock"),
		ep(inventory, "DELETE", stock, "Withdraw an offer"),
		ep(inventory, "PUT", stock+"/adjust", "Add or remove stock"),
		ep(inventory, "PUT", stock+"/price", "Set an offer's price, tax and cost"),
		ep(inventory, "GET", stock+"/movement", "An offer's stock history"),
		ep(inventory, "GET", stock+"/price-log", "An offer's price history"),
		ep(inventory, "POST", "/reservation", "Reserve stock for an order"),
		ep(inventory, "GET", "/reservation/{orderRef}", "Get a reservation"),
		ep(inventory, "PUT", "/reservation/{orderRef}/confirm", "Confirm a reservation"),
		ep(inventory, "PUT", "/reservation/{orderRef}/release", "Release a reservation"),

		// account-svc: user, role and endpoint management, and self-service
		ep(account, "GET", "/user", "List users"),
		ep(account, "POST", "/user", "Create a user"),
		ep(account, "GET", "/user/{id}", "Get a user"),
		ep(account, "PUT", "/user/{id}", "Update a user"),
		ep(account, "DELETE", "/user/{id}", "Delete a user"),
		ep(account, "PUT", "/user/{id}/password", "Change a password"),
		ep(account, "PUT", "/user/{id}/password/reset", "Reset a password without the old one"),
		ep(account, "DELETE", "/user/{id}/session", "Sign a user out everywhere"),
		ep(account, "GET", "/role", "List roles"),
		ep(account, "POST", "/role", "Create a role"),
		ep(account, "GET", "/role/{id}", "Get a role"),
		ep(account, "PUT", "/role/{id}", "Update a role"),
		ep(account, "DELETE", "/role/{id}", "Delete a role"),
		ep(account, "GET", "/endpoint", "List endpoints"),
		ep(account, "POST", "/endpoint", "Register an endpoint"),
		ep(account, "GET", "/endpoint/{id}", "Get an endpoint"),
		ep(account, "PUT", "/endpoint/{id}", "Update an endpoint"),
		ep(account, "DELETE", "/endpoint/{id}", "Delete an endpoint"),
	}
}

func grant(service string, routes ...[2]string) []string {
	out := make([]string, len(routes))
	for i, r := range routes {
		out[i] = Key(service, r[0], r[1])
	}

	return out
}

// Roles lists the platform's roles. Roles say which endpoints a user may call; the gateway's scoping rules say whose data they
// may touch, so a seller role can grant "PUT .../seller/{sellerId}/price" and still only reach the user's own seller.
//
// Staff roles (catalogue-manager) are meant for users whose attributes also say sellerScope "all": without it the gateway
// limits them like a seller, and refuses the cross-seller lists they need.
func Roles() []Role {
	const stock = "/stock/{id}/seller/{sellerId}"

	seller := append(append(
		grant(catalogue,
			[2]string{"GET", "/permissible-seller"},
			[2]string{"GET", "/seller-product-request"}, [2]string{"POST", "/seller-product-request"},
			[2]string{"GET", "/seller-product-request/{id}"}, [2]string{"PUT", "/seller-product-request/{id}"},
			[2]string{"DELETE", "/seller-product-request/{id}"}, [2]string{"PUT", "/seller-product-request/{id}/visibility"}),
		grant(inventory,
			[2]string{"GET", "/stock"}, [2]string{"GET", stock}, [2]string{"PUT", stock}, [2]string{"DELETE", stock},
			[2]string{"PUT", stock + "/adjust"}, [2]string{"PUT", stock + "/price"},
			[2]string{"GET", stock + "/movement"}, [2]string{"GET", stock + "/price-log"})...),
		grant(account,
			[2]string{"GET", "/user/{id}"}, [2]string{"PUT", "/user/{id}/password"}, [2]string{"DELETE", "/user/{id}/session"})...)

	manager := grant(catalogue,
		[2]string{"POST", "/brand"}, [2]string{"PUT", "/brand/{id}"}, [2]string{"DELETE", "/brand/{id}"},
		[2]string{"POST", "/category"}, [2]string{"PUT", "/category/{id}"}, [2]string{"DELETE", "/category/{id}"},
		[2]string{"POST", "/product"}, [2]string{"PUT", "/product/{id}"}, [2]string{"DELETE", "/product/{id}"},
		[2]string{"POST", "/variant"}, [2]string{"PUT", "/variant/{id}"}, [2]string{"DELETE", "/variant/{id}"},
		[2]string{"GET", "/seller"}, [2]string{"GET", "/seller/{id}"},
		[2]string{"PUT", "/product/{id}/permissible-seller"}, [2]string{"GET", "/permissible-seller"},
		[2]string{"GET", "/seller-product-request"}, [2]string{"GET", "/seller-product-request/{id}"},
		[2]string{"PUT", "/seller-product-request/{id}/approval"}, [2]string{"PUT", "/seller-product-request/{id}/visibility"})

	return []Role{
		{Name: "admin", Description: "Full access", AllAccess: true},
		{
			Name:        "seller",
			Description: "A seller: files product requests, manages their own offers' stock and price, and their own account",
			Grants:      seller,
		},
		{
			Name:        "catalogue-manager",
			Description: "Staff who curate the catalogue, decide who may sell what, and review seller requests (needs sellerScope all)",
			Grants:      manager,
		},
	}
}
