// Package inventory is the order service's client for inventory-svc: it reads offers (price and unit) and reserves, confirms and
// releases the stock an order holds. Reserve, Confirm and Release are idempotent in inventory-svc, so a repeat is safe.
package inventory

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/apperr"
	"github.com/zopping/order-svc/models"
)

const maxBody = 1 << 20

// Client is what the order service needs from inventory-svc. A 404 comes back as apperr.ErrNotFound and a 409 (not enough
// stock, or a reservation in the wrong state) as *apperr.StateConflict carrying inventory-svc's message.
type Client interface {
	// Offer reads one seller's offer for a variant: price, unit and status.
	Offer(ctx *gofr.Context, variantID, sellerID int64) (models.Offer, error)
	// Reserve holds the items (base units) for orderRef, all or nothing, for ttlSeconds.
	Reserve(ctx *gofr.Context, orderRef string, items []models.ReservedItem, ttlSeconds int) error
	// Confirm turns the reservation into a sale.
	Confirm(ctx *gofr.Context, orderRef string) error
	// Release gives the reserved stock back.
	Release(ctx *gofr.Context, orderRef string) error
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

func reservationPath(orderRef, action string) string {
	return fmt.Sprintf("reservation/%s/%s", orderRef, action)
}

// Offer calls GET /stock/{variant}/seller/{seller}.
func (c *HTTP) Offer(ctx *gofr.Context, variantID, sellerID int64) (models.Offer, error) {
	resp, err := ctx.GetHTTPService(c.Service).Get(ctx, offerPath(variantID, sellerID), nil)
	if err != nil {
		return models.Offer{}, fmt.Errorf("inventory offer for variant %d, seller %d: %w", variantID, sellerID, err)
	}

	defer resp.Body.Close()

	if err = check(resp); err != nil {
		return models.Offer{}, err
	}

	var body struct {
		Data models.Offer `json:"data"`
	}

	if err = json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		return models.Offer{}, fmt.Errorf("inventory offer for variant %d, seller %d: unreadable response: %w", variantID, sellerID, err)
	}

	return body.Data, nil
}

// Reserve calls POST /reservation.
func (c *HTTP) Reserve(ctx *gofr.Context, orderRef string, items []models.ReservedItem, ttlSeconds int) error {
	body, err := json.Marshal(map[string]any{"orderRef": orderRef, "items": items, "ttlSeconds": ttlSeconds})
	if err != nil {
		return err
	}

	resp, err := ctx.GetHTTPService(c.Service).Post(ctx, "reservation", nil, body)
	if err != nil {
		return fmt.Errorf("inventory reserve for order %s: %w", orderRef, err)
	}

	defer resp.Body.Close()

	return check(resp)
}

// Confirm calls PUT /reservation/{orderRef}/confirm.
func (c *HTTP) Confirm(ctx *gofr.Context, orderRef string) error {
	return c.action(ctx, orderRef, "confirm")
}

// Release calls PUT /reservation/{orderRef}/release.
func (c *HTTP) Release(ctx *gofr.Context, orderRef string) error {
	return c.action(ctx, orderRef, "release")
}

func (c *HTTP) action(ctx *gofr.Context, orderRef, action string) error {
	resp, err := ctx.GetHTTPService(c.Service).Put(ctx, reservationPath(orderRef, action), nil, nil)
	if err != nil {
		return fmt.Errorf("inventory %s for order %s: %w", action, orderRef, err)
	}

	defer resp.Body.Close()

	return check(resp)
}

// check turns a non-2xx response into an error the service layer can map.
func check(resp *http.Response) error {
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	msg := message(resp.Body)

	switch resp.StatusCode {
	case http.StatusNotFound:
		return apperr.ErrNotFound
	case http.StatusConflict:
		return &apperr.StateConflict{Reason: msg}
	default:
		return fmt.Errorf("inventory-svc: status %d: %s", resp.StatusCode, msg)
	}
}

// message extracts GoFr's {"error":{"message":...}}, falling back to the raw body.
func message(r io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(r, maxBody))

	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}

	if len(raw) > 512 {
		raw = raw[:512]
	}

	return string(raw)
}
