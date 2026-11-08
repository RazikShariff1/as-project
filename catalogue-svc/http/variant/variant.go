// Package variant holds the HTTP handlers for /variant.
package variant

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/httpx"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/services"
)

// Handler serves the variant endpoints.
type Handler struct {
	service services.Variant
}

// New returns a variant handler.
func New(service services.Variant) *Handler { return &Handler{service: service} }

// Index lists variants. Query: productId, clientItemId, barcode, status, limit, page, offset.
func (h *Handler) Index(ctx *gofr.Context) (any, error) {
	limit, offset, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	productID, err := httpx.QueryID(ctx, "productId")
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.List(ctx, &filters.VariantFilter{
		ProductID:    productID,
		ClientItemID: ctx.Param("clientItemId"),
		Barcode:      ctx.Param("barcode"),
		Status:       ctx.Param("status"),
		Limit:        limit,
		Offset:       offset,
	}))
}

// Read returns one variant.
func (h *Handler) Read(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.Get(ctx, id))
}

// Create adds a variant to a product.
func (h *Handler) Create(ctx *gofr.Context) (any, error) {
	var req models.VariantRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Create(ctx, &req))
}

// Update replaces a variant's editable fields.
func (h *Handler) Update(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.VariantRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Update(ctx, id, &req))
}

// Delete soft-deletes a variant.
func (h *Handler) Delete(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return nil, h.service.Delete(ctx, id)
}
