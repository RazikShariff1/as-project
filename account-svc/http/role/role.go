// Package role holds the HTTP handlers for /role.
package role

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/account-svc/common/httpx"
	"github.com/zopping/account-svc/filters"
	"github.com/zopping/account-svc/models"
	"github.com/zopping/account-svc/services"
)

// Handler serves the role endpoints.
type Handler struct {
	service services.Role
}

// New returns a role handler.
func New(service services.Role) *Handler { return &Handler{service: service} }

// Index lists roles. Query: name (prefix), limit, page, offset.
func (h *Handler) Index(ctx *gofr.Context) (any, error) {
	limit, offset, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.List(ctx, &filters.RoleFilter{Name: ctx.Param("name"), Limit: limit, Offset: offset}))
}

// Read returns one role with the endpoints it may call.
func (h *Handler) Read(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.Get(ctx, id))
}

// Create adds a role.
func (h *Handler) Create(ctx *gofr.Context) (any, error) {
	var req models.RoleRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Create(ctx, &req))
}

// Update replaces a role, including the endpoints it may call.
func (h *Handler) Update(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.RoleRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Update(ctx, id, &req))
}

// Delete soft-deletes a role and removes it from every user and endpoint.
func (h *Handler) Delete(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return nil, h.service.Delete(ctx, id)
}
