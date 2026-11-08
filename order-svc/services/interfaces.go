// Package services declares the business-logic contracts; implementations live in one sub-package per domain.
package services

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
)

// Order is the business-logic contract for orders.
type Order interface {
	// Place prices the items from inventory-svc, reserves their stock and stores the order.
	Place(ctx *gofr.Context, req *models.OrderRequest) (models.Order, error)
	Get(ctx *gofr.Context, id int64) (models.Order, error)
	List(ctx *gofr.Context, f *filters.OrderFilter) (models.OrderList, error)
	// UpdateStatus moves an order forward (staff only). PACKED confirms the reserved stock as a sale.
	UpdateStatus(ctx *gofr.Context, id int64, req *models.StatusRequest) (models.Order, error)
	// Cancel cancels a PLACED or CONFIRMED order and releases its stock. Repeating it retries the release.
	Cancel(ctx *gofr.Context, id int64, req *models.CancelRequest) (models.Order, error)
	// SetPaymentStatus records the payment outcome (staff and payment-svc only).
	SetPaymentStatus(ctx *gofr.Context, id int64, req *models.PaymentStatusRequest) (models.Order, error)
	Logs(ctx *gofr.Context, id int64) ([]models.OrderLog, error)
}
