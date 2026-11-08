package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/zopping/gateway-svc/accountclient"
	"github.com/zopping/gateway-svc/routing"
)

// Denied means the caller is trying to act for someone else (403).
type Denied struct{ Reason string }

func (d *Denied) Error() string { return d.Reason }

// UnsupportedMedia means a body-scoped route got a body the gateway cannot check (415).
type UnsupportedMedia struct{ Reason string }

func (u *UnsupportedMedia) Error() string { return u.Reason }

// TooLarge means the request body is bigger than the gateway will read (413).
var TooLarge = errors.New("request body too large")

type subject int

const (
	// seller: the caller may only act for the seller their account is linked to.
	seller subject = iota
	// user: the caller may only act on their own account.
	user
)

type source int

const (
	fromPath source = iota
	fromQuery
	fromBody
)

// rule says where, in a call, the caller's own id must appear. Roles say which endpoints a user may call; a rule says
// whose data they may touch, because "may call PUT /stock/{id}/seller/{sellerId}" is true of every seller.
type rule struct {
	service, method, pattern string
	who                      subject
	from                     source
	param                    string
}

func rules() []rule {
	const (
		account, catalogue, inventory = "account-svc", "catalogue-svc", "inventory-svc"
		anyMethod                     = ""
	)

	return []rule{
		// A seller's offers, stock, prices and history live under their own id.
		{inventory, anyMethod, "/stock/{id}/seller/{sellerId}", seller, fromPath, "sellerId"},
		{inventory, anyMethod, "/stock/{id}/seller/{sellerId}/**", seller, fromPath, "sellerId"},
		{inventory, "GET", "/stock", seller, fromQuery, "sellerId"},

		// A seller files and lists only their own product requests and whitelist entries. Reading, editing or deleting one by
		// id cannot be checked here (the id says nothing about its owner); catalogue-svc checks that from the forwarded identity.
		{catalogue, "POST", "/seller-product-request", seller, fromBody, "sellerId"},
		{catalogue, "GET", "/seller-product-request", seller, fromQuery, "sellerId"},
		{catalogue, "GET", "/permissible-seller", seller, fromQuery, "sellerId"},

		// Everyone may read and manage only their own account; changing someone else's is for admins.
		{account, anyMethod, "/user/{id}", user, fromPath, "id"},
		{account, anyMethod, "/user/{id}/password", user, fromPath, "id"},
		{account, anyMethod, "/user/{id}/session", user, fromPath, "id"},
	}
}

// Enforce applies the scoping rule for a call, if there is one. Callers who may act across sellers (admins, and staff with a
// sellerScope of "all") are exempt from seller rules; only admins are exempt from own-account rules.
// It returns the query string to forward, which may have the caller's own id filled in. It can read and restore the body.
func Enforce(p accountclient.Principal, service, method, path string, r *http.Request, maxBody int64) (url.Values, error) {
	query := r.URL.Query()

	for _, ru := range rules() {
		if ru.service != service || (ru.method != "" && ru.method != method) {
			continue
		}

		params, ok := routing.Match(ru.pattern, path)
		if !ok {
			continue
		}

		if exempt(p, ru.who) {
			return query, nil
		}

		id, err := subjectID(p, ru.who)
		if err != nil {
			return nil, err
		}

		return apply(ru, id, params, query, r, maxBody)
	}

	return query, nil
}

// exempt reports whether the caller is outside a rule: anyone who may act across sellers for a seller rule, and only callers with
// all access for an own-account rule (staff with a seller scope do not manage other people's accounts).
func exempt(p accountclient.Principal, who subject) bool {
	if who == user {
		return p.AllAccess
	}

	return p.AllSellers()
}

func subjectID(p accountclient.Principal, who subject) (int64, error) {
	if who == user {
		return p.User.ID, nil
	}

	id, ok := p.SellerID()
	if !ok {
		return 0, &Denied{Reason: "this account is not linked to a seller"}
	}

	return id, nil
}

func apply(ru rule, id int64, params map[string]string, query url.Values, r *http.Request, maxBody int64) (url.Values, error) {
	switch ru.from {
	case fromPath:
		if !same(params[ru.param], id) {
			return nil, denied(ru.who)
		}
	case fromQuery:
		for _, v := range query[ru.param] {
			if !same(v, id) {
				return nil, denied(ru.who)
			}
		}

		query.Set(ru.param, strconv.FormatInt(id, 10)) // a missing parameter means "everyone's": fill in the caller's own
	case fromBody:
		if err := checkBody(r, ru.param, id, maxBody); err != nil {
			return nil, err
		}
	}

	return query, nil
}

func denied(who subject) error {
	if who == user {
		return &Denied{Reason: "you can only act on your own account"}
	}

	return &Denied{Reason: "you can only act for your own seller"}
}

// same reports whether a raw path or query value is the number id. "07" and "7" are the same, as they are to the backend.
func same(raw string, id int64) bool {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)

	return err == nil && n == id
}

// checkBody makes sure no top-level JSON field named param (in any letter case) holds anything but the caller's id. Go's JSON
// decoder fills a struct field from a key of any case, so {"SELLERID": 99} reaches a sellerId field and must be caught here.
// Only JSON is accepted on these routes: GoFr also binds form bodies, which this check cannot read.
func checkBody(r *http.Request, param string, id, maxBody int64) error {
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}

	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return &UnsupportedMedia{Reason: "this endpoint accepts application/json only"}
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return err
	}

	if int64(len(raw)) > maxBody {
		return TooLarge
	}

	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))

	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil // not an object: the backend rejects it with a 400 as well
	}

	for key, value := range fields {
		if !strings.EqualFold(key, param) {
			continue
		}

		var n json.Number
		if json.Unmarshal(value, &n) != nil || !same(n.String(), id) {
			return &Denied{Reason: fmt.Sprintf("%s must be your own", param)}
		}
	}

	return nil
}
