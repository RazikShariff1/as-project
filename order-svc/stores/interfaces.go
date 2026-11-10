// Package stores declares the data-access contracts; implementations live in one sub-package per domain.
package stores

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
)

// Order is the data-access contract for orders and their items. Orders are addressed by reference number.
type Order interface {
	// Index returns a page of orders with their items, newest first.
	Index(ctx *gofr.Context, f *filters.OrderDetails, limit, offset int) ([]*models.Order, error)
	// Count returns how many orders match the filter.
	Count(ctx *gofr.Context, f *filters.OrderDetails) (int, error)
	// Retrieve returns one order with its items, or apperr.ErrNotFound.
	Retrieve(ctx *gofr.Context, ref int64) (*models.Order, error)
	// GetByClientID returns the customer's order placed with that client id, or apperr.ErrNotFound.
	GetByClientID(ctx *gofr.Context, customerID int64, clientID string) (*models.Order, error)
	// Create stores an order with its items and its OrderLogs in one transaction. A repeated (customer, client id) is
	// apperr.ErrDuplicateClient.
	Create(ctx *gofr.Context, o *models.Order) (*models.Order, error)
	// Update saves the order as given, its items and its new OrderLogs in one transaction, and returns it as stored. o.UpdatedAt
	// must be the value the order was read with: if the order was saved by someone else since, nothing is written and the answer
	// is a StateConflict.
	Update(ctx *gofr.Context, ref int64, o *models.Order) (*models.Order, error)
	// ClearStaleRef forgets a replaced reservation once inventory-svc has released it.
	ClearStaleRef(ctx *gofr.Context, ref int64, staleRef string) error
}

// OrderLog is the data-access contract for an order's history.
type OrderLog interface {
	// Create appends an entry to an existing order and returns it as stored.
	Create(ctx *gofr.Context, l *models.OrderLog) (*models.OrderLog, error)
	// GetOrderID returns the id of the order with that reference number, or apperr.ErrNotFound.
	GetOrderID(ctx *gofr.Context, referenceNumber int64) (int64, error)
	Count(ctx *gofr.Context, f *filters.OrderLog) (int, error)
	// Find returns the entries matching the filter, newest first.
	Find(ctx *gofr.Context, f *filters.OrderLog) ([]*models.OrderLog, error)
}
