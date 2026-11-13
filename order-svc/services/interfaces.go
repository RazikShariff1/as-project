// Package services declares the business-logic contracts; implementations live in one sub-package per domain.
package services

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
)

// Order is the read side of orders. Changing an order is internal/orders/services.
type Order interface {
	// Find returns a page of orders with their customers (and item names if asked). A customer only ever gets their own, whatever the filter says.
	Find(ctx *gofr.Context, filter *filters.OrderDetails, pagination *models.Pagination) (*models.OrderList, error)
	// Read returns the order with the reference number in filter.ReferenceNumbers[0], or a 404 if it is not the caller's.
	Read(ctx *gofr.Context, filter *filters.OrderDetails) (*models.OrderDetails, error)
}

// PlaceOrder places orders.
type PlaceOrder interface {
	// Create prices the items from inventory-svc, reserves their stock and stores the order. Placing again with the same clientId
	// returns the order already placed.
	Create(ctx *gofr.Context, req *models.OrderPlaceCreateSvcReq) (*models.Order, error)
}

// OrderLog is an order's history.
type OrderLog interface {
	// Create adds a staff entry to an order's history and returns its id.
	Create(ctx *gofr.Context, l *models.OrderLog) (int64, error)
	// GetOrderID resolves a reference number to the order's id, or a 404.
	GetOrderID(ctx *gofr.Context, referenceNumber int64) (int64, error)
	Count(ctx *gofr.Context, f *filters.OrderLog) (int, error)
	// Find returns the entries matching the filter, newest first.
	Find(ctx *gofr.Context, f *filters.OrderLog) ([]*models.OrderLog, error)
}
