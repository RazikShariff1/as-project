// Package services changes orders: PATCH /order/{refNum} and what it implies for stock.
package services

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/models"
)

// ItemPayload sets the quantity (in selling units) of one order line; 0 removes the line.
type ItemPayload struct {
	ID       int64
	Quantity *int64
	Reason   string
}

// PatchOrderPayload is a PATCH as the service sees it: the body, with the caller's identity resolved. Every field is optional.
type PatchOrderPayload struct {
	// UserID is who is changing the order (nil for an internal call). CustomerID, when set, says the caller is a customer: they
	// can only touch that customer's orders and only cancel, or change the address and notes.
	UserID     *int64
	CustomerID int64

	Status             string
	CancellationReason string
	Note               string
	PaymentStatus      string
	DeliveryAddress    *models.Address
	Notes              *string
	MetaData           map[string]any
	OrderItems         []*ItemPayload
}

// OrderService changes orders.
type OrderService interface {
	// Patch applies the payload to the order with the given reference number in one transaction, then does the stock calls the
	// change implies: PACKED confirms the reserved stock as a sale and CANCELLED releases it. The saved change is the truth: if a
	// stock call fails the error says so, and repeating the same PATCH retries only that call.
	Patch(ctx *gofr.Context, refNum int64, payload *PatchOrderPayload) (*models.Order, error)
}
