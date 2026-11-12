// Package communication is order-svc's client for communication-svc, which sends the emails. Sending is best effort: an order is
// never held up by, or refused because of, an email.
package communication

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/clients/lookup"
	"github.com/zopping/order-svc/models"
)

const timeout = 3 * time.Second

// Notifier tells people about orders by email.
type Notifier interface {
	// OrderPlaced sends the order confirmation to the shopper and the new-order email to the owner. It never returns an error: a
	// failure is logged.
	OrderPlaced(ctx *gofr.Context, o *models.Order)
}

// Email is one message for POST /email on communication-svc, worded by the template named in Type (the reference service's
// customer/placeOrder and owner/placeOrder).
type Email struct {
	To   string         `json:"to"`
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

// HTTP implements Notifier over a GoFr HTTP service registered under Service.
type HTTP struct {
	Service string
	Lookup  lookup.Lookup
	// Owner is the address that gets the new-order email (the admin); empty sends none.
	Owner string
	// StoreName and SupportEmail fill {{organizationName}} and {{supportEmail}}; Currency prefixes the amounts in the invoice.
	StoreName    string
	SupportEmail string
	Currency     string
}

// Emails builds the messages for a placed order: the shopper's confirmation (when their address is known) and the owner's. It is
// separate from sending so it can be tested without a network.
func (h *HTTP) Emails(o *models.Order, contact lookup.Contact, names map[int64]string) []Email {
	invoice := Invoice(o, names, h.Currency)
	common := func() map[string]any {
		return map[string]any{"invoice": invoice, "organizationName": h.StoreName, "orderNumber": strconv.FormatInt(o.ReferenceNumber, 10)}
	}

	var out []Email

	if contact.Email != "" {
		data := common()
		data["customerName"] = orDefault(contact.Name, "Customer")

		if h.SupportEmail != "" {
			data["supportEmail"] = h.SupportEmail
		}

		out = append(out, Email{To: contact.Email, Type: "customer/placeOrder", Data: data})
	}

	if h.Owner != "" {
		data := common()
		data["ownerName"] = "Admin"

		out = append(out, Email{To: h.Owner, Type: "owner/placeOrder", Data: data})
	}

	return out
}

func orDefault(v, d string) string {
	if v != "" {
		return v
	}

	return d
}

// OrderPlaced looks up what the emails need and posts them to communication-svc.
func (h *HTTP) OrderPlaced(ctx *gofr.Context, o *models.Order) {
	var contact lookup.Contact

	if o.CustomerID > 0 {
		c, err := h.Lookup.Customer(ctx, o.CustomerID)
		if err != nil {
			ctx.Warnf("order %s: no confirmation email, customer %d not found: %v", o.OrderRef, o.CustomerID, err)
		}

		contact = c
	}

	ids := make([]int64, len(o.Items))
	for i, it := range o.Items {
		ids[i] = it.VariantID
	}

	for _, e := range h.Emails(o, contact, h.Lookup.ItemNames(ctx, ids)) {
		h.send(ctx, o.OrderRef, e)
	}
}

func (h *HTTP) send(ctx *gofr.Context, orderRef string, e Email) {
	body, err := json.Marshal(e)
	if err != nil {
		return
	}

	// Its own deadline, so a slow communication-svc cannot hold the order.
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := ctx.GetHTTPService(h.Service).Post(callCtx, "email", nil, body)
	if err != nil {
		ctx.Errorf("order %s: %s email not queued: %v", orderRef, e.Type, err)

		return
	}

	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		ctx.Errorf("order %s: %s email not queued: communication-svc answered %d", orderRef, e.Type, resp.StatusCode)
	}
}

// Nop is the Notifier used when no communication-svc is configured.
type Nop struct{}

// OrderPlaced does nothing.
func (Nop) OrderPlaced(*gofr.Context, *models.Order) {}
