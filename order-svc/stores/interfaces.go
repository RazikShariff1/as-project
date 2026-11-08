// Package stores declares the data-access contracts; implementations live in one sub-package per domain.
package stores

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
)

// Order is the data-access contract for orders, their items and their history.
type Order interface {
	// Create stores an order with its items and a PLACED log entry in one transaction.
	Create(ctx *gofr.Context, o *models.Order, userID *int64) (models.Order, error)
	Get(ctx *gofr.Context, id int64) (models.Order, error)
	// List returns a page of orders (without items) and the total number matching the filter.
	List(ctx *gofr.Context, f *filters.OrderFilter) ([]models.Order, int, error)
	// Transition locks the order and moves it to status to, if its current status is one of allowed (else a StateConflict). A
	// delivered COD order is marked paid in the same transaction. For CANCELLED the note is kept as the cancel reason.
	Transition(ctx *gofr.Context, id int64, allowed []string, to, note string, userID *int64) (models.Order, error)
	// SetPaymentStatus records the payment outcome and logs it.
	SetPaymentStatus(ctx *gofr.Context, id int64, status, note string, userID *int64) (models.Order, error)
	Logs(ctx *gofr.Context, id int64) ([]models.OrderLog, error)
}
