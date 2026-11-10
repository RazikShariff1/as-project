// Package handlers holds the HTTP handler that changes orders: PATCH /order/{refNum}.
package handlers

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/caller"
	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/internal/orders/services"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/utils"
)

// PatchOrder is the body of PATCH /order/{refNum}. Every field is optional; whatever is present is applied together or not at all.
type PatchOrder struct {
	// Status moves the order forward (PACKED, DISPATCHED, COMPLETED) or cancels it (CANCELLED, from PENDING).
	Status string `json:"status"`
	// CancellationReason is kept with a CANCELLED order; Note is written to the order's history alongside the change.
	CancellationReason string `json:"cancellationReason"`
	Note               string `json:"note"`
	// PaymentStatus records the payment outcome (staff and payment-svc).
	PaymentStatus string `json:"paymentStatus"`
	// DeliveryAddress and Notes may change until the order is dispatched.
	DeliveryAddress *models.Address `json:"deliveryAddress"`
	Notes           *string         `json:"notes"`
	// MetaData is merged into the order's metadata (staff).
	MetaData map[string]any `json:"metaData"`
	// OrderItems changes quantities of a PENDING order's lines; quantity 0 removes a line (staff).
	OrderItems []*struct {
		ID       int64  `json:"orderItemId"`
		Quantity *int64 `json:"quantity"`
		Reason   string `json:"reason"`
	} `json:"orderItems"`
}

type orderHandler struct {
	service services.OrderService
}

// NewOrderHandler returns the handler for changing orders.
func NewOrderHandler(service services.OrderService) *orderHandler { //nolint:revive // as in the reference service
	return &orderHandler{service: service}
}

// Patch updates an order and answers {"order": ...}.
func (h *orderHandler) Patch(ctx *gofr.Context) (any, error) {
	refNum, err := httpx.PathRef(ctx)
	if err != nil {
		return nil, err
	}

	var payload PatchOrder
	if err = ctx.Bind(&payload); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	model := &services.PatchOrderPayload{
		UserID: utils.UserPtr(ctx), Status: payload.Status, CancellationReason: payload.CancellationReason, Note: payload.Note,
		PaymentStatus: payload.PaymentStatus, DeliveryAddress: payload.DeliveryAddress, Notes: payload.Notes, MetaData: payload.MetaData,
	}

	// A customer is restricted to their own orders; staff and internal callers are not.
	if id, restricted := caller.Customer(ctx); restricted {
		model.CustomerID = id
	}

	h.bindOrderItemDetails(&payload, model)

	order, err := h.service.Patch(ctx, refNum, model)
	if err != nil {
		return nil, err
	}

	return map[string]any{"order": order}, nil
}

func (*orderHandler) bindOrderItemDetails(payload *PatchOrder, model *services.PatchOrderPayload) {
	if len(payload.OrderItems) == 0 {
		return
	}

	for _, details := range payload.OrderItems {
		model.OrderItems = append(model.OrderItems, &services.ItemPayload{ID: details.ID, Quantity: details.Quantity, Reason: details.Reason})
	}
}
