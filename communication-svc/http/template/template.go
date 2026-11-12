// Package template holds the HTTP handlers for /communications, the email templates.
package template

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/communication-svc/common/httpx"
	"github.com/zopping/communication-svc/filters"
	"github.com/zopping/communication-svc/models"
	"github.com/zopping/communication-svc/services"
)

// Handler serves the template endpoints.
type Handler struct {
	service services.Template
}

// New returns a template handler.
func New(service services.Template) *Handler { return &Handler{service: service} }

// Index lists templates: ?name=&language=&status=&limit=&page=.
func (h *Handler) Index(ctx *gofr.Context) (any, error) {
	limit, offset, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.List(ctx, &filters.TemplateFilter{
		Name: ctx.Param("name"), Language: ctx.Param("language"), Status: ctx.Param("status"), Limit: limit, Offset: offset,
	}))
}

// Read returns one template.
func (h *Handler) Read(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.Get(ctx, id))
}

// Create stores a template (201).
func (h *Handler) Create(ctx *gofr.Context) (any, error) {
	var req models.TemplateRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Create(ctx, &req))
}

// Update replaces a template.
func (h *Handler) Update(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.TemplateRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Update(ctx, id, &req))
}

// Delete removes a template.
func (h *Handler) Delete(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return nil, h.service.Delete(ctx, id)
}
