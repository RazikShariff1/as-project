// Package store is the customer-facing front: what a signed-in (or anonymous) shopper may call through /store/{service}/...,
// and how each call is held to their own data. It is deliberately separate from package policy, which serves the team's /api
// realm: the two have different callers, different tokens, different id spaces and different rules, and a mistake in one
// must not leak into the other.
//
// Everything is an explicit allowlist. A call that is not listed is a 404; there is no "Protected by role" default here.
package store

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/zopping/gateway-svc/policy"
	"github.com/zopping/gateway-svc/routing"
)

// Prefix is where every customer-facing request starts: /store/{service}/{path...}.
const Prefix = "/store/"

// Services the shopper realm reaches. account-svc is not one of them: the team's accounts are not reachable from here.
const (
	Customers = "customer-svc"
	Orders    = "order-svc"
	Catalogue = "catalogue-svc"
	Inventory = "inventory-svc"
)

// Class is how a call is treated.
type Class int

const (
	// Public calls need no token.
	Public Class = iota
	// Signed calls need a valid shopper token.
	Signed
)

// Entry is one allowed call.
type Entry struct {
	Class Class
	// ForceQuery is set on the way through, whatever the client sent (keeps hidden catalogue entries out of listings).
	ForceQuery map[string]string
	// RateLimited calls are limited per client address (registration and login).
	RateLimited bool
	// ForwardToken sends the bearer token on to the backend, for the few customer-svc calls that identify the caller by it.
	ForwardToken bool
	// OwnQuery names a query parameter that must be the shopper's own id; a missing one is filled in.
	OwnQuery string
	// OwnBody names a JSON body field that, if present, must be the shopper's own id.
	OwnBody string
	// OnlyBody, when set, lists the only JSON body fields the shopper may send (matched in any letter case).
	OnlyBody []string
	// BodyValues restricts the value of a body field (lower-case field name to the values allowed).
	BodyValues map[string][]string
}

type route struct {
	service, method, pattern string
	entry                    Entry
}

func routes() []route {
	cust := func(e Entry) Entry { e.Class = Signed; return e }
	me := Entry{Class: Signed, ForwardToken: true}

	return []route{
		// Sign up and sign in. Rate limited like the team login.
		{Customers, "POST", "/auth/register", Entry{Class: Public, RateLimited: true}},
		{Customers, "POST", "/auth/login", Entry{Class: Public, RateLimited: true}},

		// A shopper's own account. These routes take no customer id: customer-svc resolves the caller from the token.
		{Customers, "GET", "/auth/me", me},
		{Customers, "DELETE", "/auth/session", me},
		{Customers, "GET", "/customer/me", me},
		{Customers, "PUT", "/customer/me", me},
		{Customers, "PUT", "/customer/me/password", me},
		{Customers, "GET", "/customer/me/address", me},
		{Customers, "POST", "/customer/me/address", me},
		{Customers, "GET", "/customer/me/address/{id}", me},
		{Customers, "PUT", "/customer/me/address/{id}", me},
		{Customers, "DELETE", "/customer/me/address/{id}", me},

		// Orders. order-svc restricts a shopper to their own orders from X-Customer-Id and answers 404 for anyone else's; the
		// gateway adds its own checks so a mistake there is not the only line of defence.
		{Orders, "POST", "/order", cust(Entry{OwnBody: "customerId"})},
		{Orders, "GET", "/order", cust(Entry{OwnQuery: "customerId"})},
		{Orders, "GET", "/order/{refNum}", cust(Entry{})},
		// A shopper may cancel their order and change its address and notes; status, payment, items and metadata are staff's.
		{Orders, "PATCH", "/order/{refNum}", cust(Entry{
			OnlyBody:   []string{"status", "cancellationReason", "note", "deliveryAddress", "notes"},
			BodyValues: map[string][]string{"status": {"CANCELLED"}},
		})},
		{Orders, "GET", "/order-log", cust(Entry{})},
	}
}

// Lookup finds the entry for a call. The catalogue and stock reads that anyone may make are the same ones the team realm
// leaves public (policy.Classify); everything else must be in the table above.
func Lookup(service, method, path string) (Entry, bool) {
	switch service {
	case Catalogue, Inventory:
		if class, force := policy.Classify(service, method, path); class == policy.Public {
			return Entry{Class: Public, ForceQuery: force}, true
		}

		return Entry{}, false
	}

	for _, r := range routes() {
		if r.service != service || r.method != method {
			continue
		}

		if _, ok := routing.Match(r.pattern, path); ok {
			return r.entry, true
		}
	}

	return Entry{}, false
}

// Enforce holds a signed call to the shopper's own data and returns the query string to forward. It may read and restore the body.
func Enforce(e Entry, customerID int64, r *http.Request, maxBody int64) (url.Values, error) {
	query := r.URL.Query()

	for k, v := range e.ForceQuery {
		query.Set(k, v)
	}

	if e.OwnQuery != "" {
		for _, v := range query[e.OwnQuery] {
			if n, err := strconv.ParseInt(v, 10, 64); err != nil || n != customerID {
				return nil, &policy.Denied{Reason: "you can only see your own " + e.OwnQuery}
			}
		}

		query.Set(e.OwnQuery, strconv.FormatInt(customerID, 10)) // a missing parameter means "everyone's": fill in the caller's own
	}

	if e.OwnBody != "" || e.OnlyBody != nil {
		if err := checkBody(r, e, customerID, maxBody); err != nil {
			return nil, err
		}
	}

	return query, nil
}
