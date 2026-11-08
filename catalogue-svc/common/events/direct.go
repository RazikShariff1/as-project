package events

import (
	"errors"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/models"
)

// Direct delivers seller product change events in-process, with no broker: the handler runs inside the publishing request.
// The service is both publisher and subscriber of this event, so a broker only adds a round trip.
type Direct struct {
	handle func(ctx *gofr.Context, requestIDs []int64) error
}

// NewDirect returns a publisher that passes each event straight to handle.
func NewDirect(handle func(ctx *gofr.Context, requestIDs []int64) error) Direct {
	return Direct{handle: handle}
}

// Publish applies the event now. An error means it was not applied; the request stays unsynced for reconcile.
func (d Direct) Publish(ctx *gofr.Context, _ string, payload any) error {
	e, ok := payload.(models.SellerProductChangeEvent)
	if !ok {
		return errors.New("direct delivery only handles seller product change events")
	}

	return d.handle(ctx, e.SellerProductRequestIDs)
}
