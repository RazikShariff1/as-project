// Package brand holds the HTTP handlers for /brand.
package brand

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/httpx"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/services"
)

// Handler serves the brand endpoints.
type Handler struct {
	service services.Brand
}

// New returns a brand handler.
func New(service services.Brand) *Handler { return &Handler{service: service} }

// Index lists brands. Query: name (prefix), slug, clientId, status, limit, page, offset.
func (h *Handler) Index(ctx *gofr.Context) (any, error) {
	limit, offset, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.List(ctx, &filters.BrandFilter{
		Name:     ctx.Param("name"),
		Slug:     ctx.Param("slug"),
		ClientID: ctx.Param("clientId"),
		Status:   ctx.Param("status"),
		Limit:    limit,
		Offset:   offset,
	}))
}

// Read returns one brand.
func (h *Handler) Read(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.Get(ctx, id))
}

// Create adds a brand.
func (h *Handler) Create(ctx *gofr.Context) (any, error) {
	var req models.BrandRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Create(ctx, &req))
}

// Update replaces a brand's editable fields.
func (h *Handler) Update(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.BrandRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Update(ctx, id, &req))
}

// Delete soft-deletes a brand.
func (h *Handler) Delete(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return nil, h.service.Delete(ctx, id)
}
