// Package catalogue is the order service's client for catalogue-svc: it reads the names of what an order holds.
package catalogue

import (
	"fmt"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/models"
)

// Client is what the order service needs from catalogue-svc.
type Client interface {
	// Variant reads one variant: its name and the product it belongs to.
	Variant(ctx *gofr.Context, id int64) (*models.CatalogueVariant, error)
	// Product reads one product: its name.
	Product(ctx *gofr.Context, id int64) (*models.CatalogueProduct, error)
}

// HTTP implements Client over a GoFr HTTP service registered under Service.
type HTTP struct {
	Service string
}

// New returns a client for the HTTP service registered under the given name.
func New(service string) *HTTP { return &HTTP{Service: service} }

// Variant calls GET /variant/{id}.
func (c *HTTP) Variant(ctx *gofr.Context, id int64) (*models.CatalogueVariant, error) {
	var out models.CatalogueVariant

	if err := httpx.GetData(ctx, c.Service, fmt.Sprintf("variant/%d", id), &out); err != nil {
		return nil, err
	}

	return &out, nil
}

// Product calls GET /product/{id}.
func (c *HTTP) Product(ctx *gofr.Context, id int64) (*models.CatalogueProduct, error) {
	var out models.CatalogueProduct

	if err := httpx.GetData(ctx, c.Service, fmt.Sprintf("product/%d", id), &out); err != nil {
		return nil, err
	}

	return &out, nil
}
