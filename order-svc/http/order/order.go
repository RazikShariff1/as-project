// Package order holds the HTTP handlers for /order.
package order

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/services"
)

// Handler serves the order endpoints.
type Handler struct {
	service services.Order
}

// New returns an order handler.
func New(service services.Order) *Handler { return &Handler{service: service} }

// Create places an order.
func (h *Handler) Create(ctx *gofr.Context) (any, error) {
	var req models.OrderRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Place(ctx, &req))
}

// Index lists orders, newest first. Query: status, customerId (staff only; customers always get their own), limit, page, offset.
func (h *Handler) Index(ctx *gofr.Context) (any, error) {
	limit, offset, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	customerID, err := httpx.QueryID(ctx, "customerId")
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.List(ctx, &filters.OrderFilter{
		CustomerID: customerID, Status: ctx.Param("status"), Limit: limit, Offset: offset,
	}))
}

// Read returns an order with its items.
func (h *Handler) Read(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.Get(ctx, id))
}

// UpdateStatus moves an order to the next status (staff).
func (h *Handler) UpdateStatus(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.StatusRequest
	if err = ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.UpdateStatus(ctx, id, &req))
}

// Cancel cancels an order that has not been packed.
func (h *Handler) Cancel(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.CancelRequest
	if err = ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Cancel(ctx, id, &req))
}

// PaymentStatus records the payment outcome (staff and payment-svc).
func (h *Handler) PaymentStatus(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.PaymentStatusRequest
	if err = ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.SetPaymentStatus(ctx, id, &req))
}

// Logs returns an order's history.
func (h *Handler) Logs(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.Logs(ctx, id))
}
