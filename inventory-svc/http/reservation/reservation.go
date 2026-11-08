// Package reservation holds the HTTP handlers for /reservation.
package reservation

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/inventory-svc/common/httpx"
	"github.com/zopping/inventory-svc/models"
	"github.com/zopping/inventory-svc/services"
)

// Handler serves the reservation endpoints. The {orderRef} in each path is the caller's own order reference.
type Handler struct {
	service services.Reservation
}

// New returns a reservation handler.
func New(service services.Reservation) *Handler { return &Handler{service: service} }

// Create reserves stock for every item of an order, or for none (409 if any item cannot be covered).
func (h *Handler) Create(ctx *gofr.Context) (any, error) {
	var req models.ReservationRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Create(ctx, &req))
}

// Read returns a reservation.
func (h *Handler) Read(ctx *gofr.Context) (any, error) {
	return httpx.Respond(h.service.Get(ctx, ctx.PathParam("orderRef")))
}

// Confirm turns a reservation into a sale. Repeating it is safe.
func (h *Handler) Confirm(ctx *gofr.Context) (any, error) {
	return httpx.Respond(h.service.Confirm(ctx, ctx.PathParam("orderRef")))
}

// Release gives a reservation's stock back. Repeating it is safe; a confirmed reservation cannot be released.
func (h *Handler) Release(ctx *gofr.Context) (any, error) {
	return httpx.Respond(h.service.Release(ctx, ctx.PathParam("orderRef")))
}

// ReleaseExpired releases holds that ran past their expiry; meant for a scheduler. Query: limit (default 20, max 100).
func (h *Handler) ReleaseExpired(ctx *gofr.Context) (any, error) {
	limit, _, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	released, err := h.service.ReleaseExpired(ctx, limit)
	if err != nil {
		return nil, err
	}

	return map[string]int{"released": released}, nil
}
