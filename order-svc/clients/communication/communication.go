// Package communication is order-svc's client for communication-svc, which sends the emails. Sending is best effort: an order is
// never held up by, or refused because of, an email.
package communication

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/models"
)

const timeout = 3 * time.Second

// Notifier tells people about orders by email.
type Notifier interface {
	// OrderPlaced tells the owner a new order came in. It never returns an error: a failure is logged.
	OrderPlaced(ctx *gofr.Context, o *models.Order)
}

// HTTP implements Notifier over a GoFr HTTP service registered under Service. For now every email goes to Owner, the admin.
type HTTP struct {
	Service string
	Owner   string
}

// New returns a notifier that emails owner through the HTTP service registered under the given name.
func New(service, owner string) *HTTP { return &HTTP{Service: service, Owner: owner} }

// OrderPlaced posts an "owner/placeOrder" email to communication-svc.
func (h *HTTP) OrderPlaced(ctx *gofr.Context, o *models.Order) {
	items := make([]map[string]any, len(o.Items))
	for i, it := range o.Items {
		items[i] = map[string]any{"variantId": it.VariantID, "quantity": it.Quantity, "unitPrice": it.UnitPrice, "lineTotal": it.LineTotal}
	}

	body, err := json.Marshal(map[string]any{"to": h.Owner, "type": "owner/placeOrder", "data": map[string]any{
		"orderRef": o.OrderRef, "referenceNumber": o.ReferenceNumber, "customerId": o.CustomerID,
		"paymentMethod": o.PaymentMethod, "total": o.Total, "items": items,
	}})
	if err != nil {
		return
	}

	// Its own deadline, so a slow communication-svc cannot hold the order.
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := ctx.GetHTTPService(h.Service).Post(callCtx, "email", nil, body)
	if err != nil {
		ctx.Errorf("order %s: email to the owner not queued: %v", o.OrderRef, err)

		return
	}

	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		ctx.Errorf("order %s: email to the owner not queued: communication-svc answered %d", o.OrderRef, resp.StatusCode)
	}
}

// Nop is the Notifier used when no communication-svc or no owner address is configured.
type Nop struct{}

// OrderPlaced does nothing.
func (Nop) OrderPlaced(*gofr.Context, *models.Order) {}
