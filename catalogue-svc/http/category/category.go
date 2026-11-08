// Package category holds the HTTP handlers for /category.
package category

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/httpx"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/services"
)

// Handler serves the category endpoints.
type Handler struct {
	service services.Category
}

// New returns a category handler.
func New(service services.Category) *Handler { return &Handler{service: service} }

// Index lists categories. Query: parentCategoryId, slug, clientId, status, limit, page, offset.
func (h *Handler) Index(ctx *gofr.Context) (any, error) {
	limit, offset, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	parentID, err := httpx.QueryID(ctx, "parentCategoryId")
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.List(ctx, &filters.CategoryFilter{
		ParentCategoryID: parentID,
		Slug:             ctx.Param("slug"),
		ClientID:         ctx.Param("clientId"),
		Status:           ctx.Param("status"),
		Limit:            limit,
		Offset:           offset,
	}))
}

// Read returns one category.
func (h *Handler) Read(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.Get(ctx, id))
}

// Create adds a category.
func (h *Handler) Create(ctx *gofr.Context) (any, error) {
	var req models.CategoryRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Create(ctx, &req))
}

// Update replaces a category's name, slug and parent.
func (h *Handler) Update(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.CategoryRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Update(ctx, id, &req))
}

// Delete soft-deletes a category.
func (h *Handler) Delete(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return nil, h.service.Delete(ctx, id)
}
