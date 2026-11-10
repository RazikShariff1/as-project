// Package customerclient asks customer-svc who a shopper's bearer token belongs to. It is the customer realm's counterpart
// of accountclient: shoppers and team members are different people with different tokens and different id spaces.
package customerclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zopping/gateway-svc/accountclient"
)

const (
	maxResponse = 1 << 20
	timeout     = 5 * time.Second
)

// Customer is the part of a shopper's account the gateway needs.
type Customer struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

// Principal is who a valid shopper token belongs to.
type Principal struct {
	Customer Customer `json:"customer"`
}

// Client asks customer-svc about a token.
type Client interface {
	// Me authenticates the token. customer-svc refuses a suspended shopper (403) and an unknown or expired token (401); the
	// refusal is returned as an *accountclient.Error so the gateway passes it on unchanged. An unreachable customer-svc is
	// accountclient.ErrUnavailable: the request is refused, never let through.
	Me(ctx context.Context, token string) (Principal, error)
}

// HTTP implements Client over HTTP.
type HTTP struct {
	base   *url.URL
	client *http.Client
}

// New returns a client for the customer service at base.
func New(base *url.URL) *HTTP {
	return &HTTP{base: base, client: &http.Client{Timeout: timeout}}
}

// Me calls GET /auth/me.
func (c *HTTP) Me(ctx context.Context, token string) (Principal, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.base.String(), "/")+"/auth/me", nil)
	if err != nil {
		return Principal{}, accountclient.ErrUnavailable
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.client.Do(req)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: %v", accountclient.ErrUnavailable, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil || resp.StatusCode >= http.StatusInternalServerError {
		return Principal{}, accountclient.ErrUnavailable
	}

	if resp.StatusCode != http.StatusOK {
		return Principal{}, &accountclient.Error{Status: resp.StatusCode, Body: body}
	}

	var envelope struct {
		Data Principal `json:"data"`
	}

	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Data.Customer.ID < 1 {
		return Principal{}, accountclient.ErrUnavailable
	}

	return envelope.Data, nil
}
