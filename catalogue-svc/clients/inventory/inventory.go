// Package inventory is the catalogue's client for inventory-svc: it makes a seller's offer live or hides it.
package inventory

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"gofr.dev/pkg/gofr"
)

const maxErrorBody = 512

// Client makes inventory-svc match an approved seller product request. Both calls are idempotent, because the event that
// triggers them is delivered at least once.
type Client interface {
	// UpsertOffer creates the seller's offer for the variant, or updates its mrp and discount and makes it visible. It
	// never changes stock.
	UpsertOffer(ctx *gofr.Context, variantID, sellerID int64, mrp, discount float64, note string) error
	// Withdraw hides the seller's offer for the variant; an offer that does not exist is not an error.
	Withdraw(ctx *gofr.Context, variantID, sellerID int64) error
}

// HTTP implements Client over a GoFr HTTP service registered under Service.
type HTTP struct {
	Service string
}

// New returns a client for the HTTP service registered under the given name.
func New(service string) *HTTP { return &HTTP{Service: service} }

func offerPath(variantID, sellerID int64) string {
	return fmt.Sprintf("stock/%d/seller/%d", variantID, sellerID)
}

// UpsertOffer calls PUT /stock/{variant}/seller/{seller}/offer.
func (c *HTTP) UpsertOffer(ctx *gofr.Context, variantID, sellerID int64, mrp, discount float64, note string) error {
	body, err := json.Marshal(map[string]any{"mrp": mrp, "discount": discount, "notes": note})
	if err != nil {
		return err
	}

	resp, err := ctx.GetHTTPService(c.Service).Put(ctx, offerPath(variantID, sellerID)+"/offer", nil, body)
	if err != nil {
		return fmt.Errorf("inventory offer for variant %d, seller %d: %w", variantID, sellerID, err)
	}

	return check(resp, fmt.Sprintf("offer for variant %d, seller %d", variantID, sellerID), false)
}

// Withdraw calls DELETE /stock/{variant}/seller/{seller}.
func (c *HTTP) Withdraw(ctx *gofr.Context, variantID, sellerID int64) error {
	resp, err := ctx.GetHTTPService(c.Service).Delete(ctx, offerPath(variantID, sellerID), nil)
	if err != nil {
		return fmt.Errorf("inventory withdraw for variant %d, seller %d: %w", variantID, sellerID, err)
	}

	return check(resp, fmt.Sprintf("withdraw for variant %d, seller %d", variantID, sellerID), true)
}

// check turns a non-2xx response into an error (so the event is redelivered), optionally accepting 404.
func check(resp *http.Response, what string, notFoundOK bool) error {
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	if notFoundOK && resp.StatusCode == http.StatusNotFound {
		return nil
	}

	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))

	return fmt.Errorf("inventory-svc %s: status %d: %s", what, resp.StatusCode, b)
}
