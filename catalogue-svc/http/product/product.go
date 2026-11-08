// Package product holds the HTTP handlers for /product.
package product

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/httpx"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/services"
)

// Handler serves the product endpoints.
type Handler struct {
	service services.Product
}

// New returns a product handler.
func New(service services.Product) *Handler { return &Handler{service: service} }

// Index lists products. Query: categoryId, brandId, status, clientItemId, barcode, q (search), limit, page, offset.
func (h *Handler) Index(ctx *gofr.Context) (any, error) {
	limit, offset, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	categoryID, err := httpx.QueryID(ctx, "categoryId")
	if err != nil {
		return nil, err
	}

	brandID, err := httpx.QueryID(ctx, "brandId")
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.List(ctx, &filters.ProductFilter{
		CategoryID:   categoryID,
		BrandID:      brandID,
		Status:       ctx.Param("status"),
		ClientItemID: ctx.Param("clientItemId"),
		Barcode:      ctx.Param("barcode"),
		Query:        ctx.Param("q"),
		Limit:        limit,
		Offset:       offset,
	}))
}

// Read returns one product with its variants.
func (h *Handler) Read(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.Get(ctx, id))
}

// Create adds a product.
func (h *Handler) Create(ctx *gofr.Context) (any, error) {
	var req models.ProductRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Create(ctx, &req))
}

// Update replaces a product's editable fields.
func (h *Handler) Update(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.ProductRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Update(ctx, id, &req))
}

// Delete soft-deletes a product and its variants.
func (h *Handler) Delete(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return nil, h.service.Delete(ctx, id)
}
