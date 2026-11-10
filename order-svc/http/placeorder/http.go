// Package placeorder holds the HTTP handler for POST /order.
package placeorder

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/caller"
	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/services"
	"github.com/zopping/order-svc/utils"
)

// Handler serves POST /order.
type Handler struct {
	placeOrderSvc services.PlaceOrder
}

// New returns a place-order handler.
func New(placeOrderSvc services.PlaceOrder) *Handler {
	return &Handler{placeOrderSvc: placeOrderSvc}
}

// Create places an order and answers {"order": ...}.
func (h *Handler) Create(ctx *gofr.Context) (any, error) {
	var orderCreateRequest models.OrderPlaceCreateHTTP
	if err := ctx.Bind(&orderCreateRequest); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	res, err := h.placeOrderSvc.Create(ctx, getOrderPlaceCreateSvc(ctx, &orderCreateRequest))
	if err != nil {
		return nil, err
	}

	return struct {
		Order *models.Order `json:"order"`
	}{Order: res}, nil
}

// getOrderPlaceCreateSvc turns the HTTP body into the service request: a customer always orders for themselves, whatever the body
// says; staff and internal callers name the customer.
func getOrderPlaceCreateSvc(ctx *gofr.Context, req *models.OrderPlaceCreateHTTP) *models.OrderPlaceCreateSvcReq {
	customerID := req.CustomerID
	if id, restricted := caller.Customer(ctx); restricted {
		customerID = id
	}

	return &models.OrderPlaceCreateSvcReq{
		CustomerID: customerID, UserID: utils.UserPtr(ctx), ClientID: req.ClientID, MetaData: req.MetaData, Items: req.Items,
		Address: req.DeliveryAddress, PaymentMethod: req.PaymentMethod, Notes: req.Notes,
	}
}
