// Package sellerchange holds the HTTP handler a scheduler calls to repair seller product requests that never reached
// inventory-svc.
package sellerchange

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/httpx"
	"github.com/zopping/catalogue-svc/services"
)

// Handler serves the reconcile endpoint.
type Handler struct {
	service services.SellerChange
}

// New returns a reconcile handler.
func New(service services.SellerChange) *Handler { return &Handler{service: service} }

// Reconcile applies approved or archived requests whose live state has not reached inventory yet, and reports how many it
// applied. Call it on a schedule (every minute is plenty); repeating it is safe. Query: limit (default 20, max 100).
func (h *Handler) Reconcile(ctx *gofr.Context) (any, error) {
	limit, _, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	applied, err := h.service.Reconcile(ctx, limit)
	if err != nil {
		return nil, err
	}

	return map[string]int{"applied": applied}, nil
}
