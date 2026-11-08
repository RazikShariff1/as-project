// Package accountclient asks account-svc who a bearer token belongs to and whether they may make a call.
package accountclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxResponse = 1 << 20
	timeout     = 5 * time.Second
)

// ErrUnavailable means account-svc could not be reached or answered nonsense. The gateway then refuses the request: it never
// lets a request through because authentication was down.
var ErrUnavailable = errors.New("authentication service unavailable")

// User is the part of an account the gateway needs.
type User struct {
	ID         int64          `json:"id"`
	Attributes map[string]any `json:"attributes"`
	RoleIDs    []int64        `json:"roleIds"`
}

// Principal is who a valid token belongs to. AllAccess means one of their roles may call everything.
type Principal struct {
	User      User `json:"user"`
	AllAccess bool `json:"allAccess"`
}

// SellerID returns the seller the account is linked to (its sellerId attribute). It is false when the attribute is missing
// or is not a whole positive number, so a badly configured account has no seller rather than a wrong one.
func (p Principal) SellerID() (int64, bool) {
	f, ok := p.User.Attributes["sellerId"].(float64)
	if !ok || f < 1 || f != float64(int64(f)) {
		return 0, false
	}

	return int64(f), true
}

// AllSellers reports whether the account may act across sellers: it has all access, or an explicit sellerScope of "all" (staff
// such as a catalogue manager). A seller account has neither, so it is limited to its own seller. This is what exempts a call from
// the seller scoping rules; having a role that grants an endpoint does not.
func (p Principal) AllSellers() bool {
	scope, _ := p.User.Attributes["sellerScope"].(string)

	return p.AllAccess || scope == "all"
}

// Error is account-svc refusing the request (401, 403, 429 ...). The gateway passes the status and body to the client as is.
type Error struct {
	Status int
	Body   []byte
}

func (e *Error) Error() string { return fmt.Sprintf("account-svc answered %d", e.Status) }

// Client asks account-svc about a token.
type Client interface {
	// Check authenticates the token and checks the user may call method on path of service.
	Check(ctx context.Context, token, service, method, path string) (Principal, error)
	// Me authenticates the token without a permission check, for the few calls every signed-in user may make.
	Me(ctx context.Context, token string) (Principal, error)
}

// HTTP implements Client over HTTP.
type HTTP struct {
	base   *url.URL
	client *http.Client
}

// New returns a client for the account service at base.
func New(base *url.URL) *HTTP {
	return &HTTP{base: base, client: &http.Client{Timeout: timeout}}
}

// Check calls GET /auth/check.
func (c *HTTP) Check(ctx context.Context, token, service, method, path string) (Principal, error) {
	q := url.Values{"service": {service}, "method": {method}, "path": {path}}

	return c.get(ctx, "/auth/check?"+q.Encode(), token)
}

// Me calls GET /auth/me.
func (c *HTTP) Me(ctx context.Context, token string) (Principal, error) {
	return c.get(ctx, "/auth/me", token)
}

func (c *HTTP) get(ctx context.Context, pathAndQuery, token string) (Principal, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.base.String(), "/")+pathAndQuery, nil)
	if err != nil {
		return Principal{}, ErrUnavailable
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.client.Do(req)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return Principal{}, ErrUnavailable
	}

	if resp.StatusCode >= http.StatusInternalServerError {
		return Principal{}, ErrUnavailable
	}

	if resp.StatusCode != http.StatusOK {
		return Principal{}, &Error{Status: resp.StatusCode, Body: body}
	}

	var envelope struct {
		Data Principal `json:"data"`
	}

	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Data.User.ID < 1 {
		return Principal{}, ErrUnavailable
	}

	return envelope.Data, nil
}
