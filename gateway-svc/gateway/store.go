package gateway

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/zopping/gateway-svc/routing"
	"github.com/zopping/gateway-svc/store"
)

// serveStore handles the customer-facing realm: /store/{service}/{path}. It is a fixed allowlist (package store): a call that is
// not listed is a 404, a signed call needs a live shopper token from customer-svc, and every signed call is held to the shopper's
// own data before it is proxied with X-Customer-Id (never X-User-Id: shoppers are not team accounts).
func (g *Gateway) serveStore(w http.ResponseWriter, r *http.Request) {
	if g.customers == nil {
		writeError(w, http.StatusNotFound, "not found")

		return
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent) // CORS preflight carries no credentials

		return
	}

	service, rest, ok := routing.ParseWith(store.Prefix, r.URL.Path)
	target := g.settings.Services[service]

	note(r, func(l *record) { l.service, l.path = service, rest })

	if !ok || target == nil {
		note(r, func(l *record) { l.decision = "unknown-service" })
		writeError(w, http.StatusNotFound, "unknown service")

		return
	}

	rest, ok = cleanPath(rest)
	if !ok {
		note(r, func(l *record) { l.decision = "invalid-path" })
		writeError(w, http.StatusBadRequest, "invalid path")

		return
	}

	note(r, func(l *record) { l.path = rest })

	// Read the token before the credentials are stripped from what the backend will see.
	token := bearer(r)

	stripIdentity(r.Header)

	if r.Header.Get(correlationID) == "" {
		r.Header.Set(correlationID, newID())
	}

	w.Header().Set(correlationID, r.Header.Get(correlationID))

	entry, ok := store.Lookup(service, r.Method, rest)
	if !ok {
		note(r, func(l *record) { l.decision = "not-listed" })
		writeError(w, http.StatusNotFound, "not found") // not listed, so not reachable; not revealed either

		return
	}

	decision := "allowlisted-public"
	if entry.Class == store.Signed {
		decision = "allowlisted-signed"
	}

	note(r, func(l *record) { l.decision = decision })

	if entry.RateLimited && !g.storeRate.Allow(g.clientIP(r)) {
		note(r, func(l *record) { l.decision = "rate-limited" })
		writeError(w, http.StatusTooManyRequests, "too many attempts; try again in a minute")

		return
	}

	if entry.Class == store.Public {
		query := r.URL.Query()
		for k, v := range entry.ForceQuery {
			query.Set(k, v)
		}

		g.proxy(w, r, target, rest, query, nil, "")

		return
	}

	g.serveSigned(w, r, target, rest, token, entry)
}

func (g *Gateway) serveSigned(w http.ResponseWriter, r *http.Request, target *url.URL, rest, token string, entry store.Entry) {
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing token")

		return
	}

	p, err := g.customers.Me(r.Context(), token)
	if err != nil {
		writeAccountError(w, err) // customer-svc's own 401/403 pass through; an outage refuses the request

		return
	}

	note(r, func(l *record) { l.who = "customer:" + strconv.FormatInt(p.Customer.ID, 10) })

	query, err := store.Enforce(entry, p.Customer.ID, r, g.settings.MaxBodyBytes)
	if err != nil {
		note(r, func(l *record) { l.decision = "own-data-denied" })
		writeScopeError(w, err)

		return
	}

	// customer-svc identifies the caller of its own /auth and /customer/me routes by the token, so only those get it forwarded.
	forward := ""
	if entry.ForwardToken {
		forward = token
	}

	g.proxy(w, r, target, rest, query, func(h http.Header) { setCustomerIdentity(h, p.Customer.ID) }, forward)
}
