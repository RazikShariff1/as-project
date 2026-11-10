// Package order holds the HTTP handlers that read orders: GET /order and GET /order/{refNum}.
package order

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/services"
)

// Handler serves the order read endpoints.
type Handler struct {
	orderService services.Order
}

// New returns an order handler.
func New(orderService services.Order) *Handler {
	return &Handler{orderService: orderService}
}

// orderReadResponse answers a single order as {"order": ...}, as the reference service does.
type orderReadResponse struct {
	Order models.Order `json:"order"`
}

// Index lists orders, newest first. Query: status and referenceNumber (comma-separated), paymentStatus, placedBetween (from,to
// dates), customerId (staff only; customers always get their own), limit, page, offset. It answers {"count", "order": [...]}.
func (h *Handler) Index(ctx *gofr.Context) (any, error) {
	filter, err := orderFilter(ctx)
	if err != nil {
		return nil, err
	}

	limit, offset, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.orderService.Find(ctx, filter, &models.Pagination{Limit: limit, Offset: offset}))
}

// Read returns one order with its items.
func (h *Handler) Read(ctx *gofr.Context) (any, error) {
	ref, err := httpx.PathRef(ctx)
	if err != nil {
		return nil, err
	}

	order, err := h.orderService.Read(ctx, &filters.OrderDetails{ReferenceNumbers: []int64{ref}})
	if err != nil {
		return nil, err
	}

	return orderReadResponse{Order: *order}, nil
}
