package gateway

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/zopping/gateway-svc/accountclient"
)

// The identity the backends receive. They have no auth of their own and trust these headers, which is safe only because
// the gateway removes anything a client sends under these names and is the only way in.
const (
	headerUserID    = "X-User-Id"
	headerSellerID  = "X-Seller-Id"
	headerAllAccess = "X-All-Access"
	headerRoleIDs   = "X-Role-Ids"
	// headerSellerScope is "all" when the caller may act across sellers. Backends limit a caller to X-Seller-Id unless it is set.
	headerSellerScope = "X-Seller-Scope"
)

// stripped are the header name prefixes a client may never send to a backend.
func stripped() []string {
	return []string{"X-User-", "X-Seller-", "X-All-", "X-Role-", "X-Identity-", "X-Forwarded-User"}
}

// stripIdentity deletes every header a client could use to pose as someone else, plus the credentials themselves: the
// backends need neither the token nor cookies.
func stripIdentity(h http.Header) {
	for name := range h {
		for _, prefix := range stripped() {
			if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
				delete(h, name)

				break
			}
		}
	}

	h.Del("Authorization")
	h.Del("Cookie")
}

// setIdentity adds the verified identity of p to the headers sent to a backend.
func setIdentity(h http.Header, p accountclient.Principal) {
	h.Set(headerUserID, strconv.FormatInt(p.User.ID, 10))

	if id, ok := p.SellerID(); ok {
		h.Set(headerSellerID, strconv.FormatInt(id, 10))
	}

	if p.AllAccess {
		h.Set(headerAllAccess, "true")
	}

	if p.AllSellers() {
		h.Set(headerSellerScope, "all")
	}

	roles := make([]string, len(p.User.RoleIDs))
	for i, id := range p.User.RoleIDs {
		roles[i] = strconv.FormatInt(id, 10)
	}

	if len(roles) > 0 {
		h.Set(headerRoleIDs, strings.Join(roles, ","))
	}
}
