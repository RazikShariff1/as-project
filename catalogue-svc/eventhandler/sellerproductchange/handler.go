// Package sellerproductchange is the pub/sub subscriber for seller product change events.
package sellerproductchange

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/services"
)

// Subscriber handles seller product change events.
type Subscriber struct {
	service services.SellerChange
}

// New returns a subscriber.
func New(service services.SellerChange) *Subscriber { return &Subscriber{service: service} }

// Handle processes one event and always acknowledges it. A malformed message can never be fixed by retrying, so it is
// logged and dropped. A failed apply (inventory-svc down, say) is logged and acknowledged too, on purpose: with GoFr's Google
// backend an error return does not make the broker redeliver; it leaves the message unacknowledged while the client library
// keeps extending its deadline (an hour by default), so nothing would retry. The request stays unsynced in the database
// instead, and the reconcile endpoint, which a scheduler calls, applies it later.
func (s *Subscriber) Handle(ctx *gofr.Context) error {
	var event models.SellerProductChangeEvent
	if err := ctx.Bind(&event); err != nil {
		ctx.Errorf("dropping a malformed seller product change event: %v", err)

		return nil
	}

	if err := s.service.Process(ctx, event.SellerProductRequestIDs); err != nil {
		ctx.Errorf("could not apply seller product requests %v to inventory; they stay unsynced until the next reconcile: %v",
			event.SellerProductRequestIDs, err)
	}

	return nil
}
