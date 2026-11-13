// Package customer is the order service's client for customer-svc: it reads the shopper behind an order.
package customer

import (
	"fmt"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/models"
)

// Client is what the order service needs from customer-svc.
type Client interface {
	// Customer reads one customer by id.
	Customer(ctx *gofr.Context, id int64) (*models.Customer, error)
}

// HTTP implements Client over a GoFr HTTP service registered under Service.
type HTTP struct {
	Service string
}

// New returns a client for the HTTP service registered under the given name.
func New(service string) *HTTP { return &HTTP{Service: service} }

// Customer calls GET /customer/{id}.
func (c *HTTP) Customer(ctx *gofr.Context, id int64) (*models.Customer, error) {
	var out models.Customer

	if err := httpx.GetData(ctx, c.Service, fmt.Sprintf("customer/%d", id), &out); err != nil {
		return nil, err
	}

	return &out, nil
}
