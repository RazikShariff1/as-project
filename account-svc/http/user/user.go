// Package user holds the HTTP handlers for /user.
package user

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/account-svc/common/httpx"
	"github.com/zopping/account-svc/filters"
	"github.com/zopping/account-svc/models"
	"github.com/zopping/account-svc/services"
)

// Handler serves the user endpoints.
type Handler struct {
	service services.User
}

// New returns a user handler.
func New(service services.User) *Handler { return &Handler{service: service} }

// Index lists users. Query: name and email (prefix), status, roleId, limit, page, offset.
func (h *Handler) Index(ctx *gofr.Context) (any, error) {
	limit, offset, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	roleID, err := httpx.QueryID(ctx, "roleId")
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.List(ctx, &filters.UserFilter{
		Name: ctx.Param("name"), Email: ctx.Param("email"), Status: ctx.Param("status"), RoleID: roleID,
		Limit: limit, Offset: offset,
	}))
}

// Read returns one user.
func (h *Handler) Read(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.Get(ctx, id))
}

// Create adds a user with a password.
func (h *Handler) Create(ctx *gofr.Context) (any, error) {
	var req models.UserRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Create(ctx, &req))
}

// Update replaces a user's profile, status, attributes and roles. It never changes the password.
func (h *Handler) Update(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.UserRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Update(ctx, id, &req))
}

// Delete soft-deletes a user and signs them out everywhere.
func (h *Handler) Delete(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return nil, h.service.Delete(ctx, id)
}

// ChangePassword sets a new password after checking the current one, and signs the user out everywhere.
func (h *Handler) ChangePassword(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.PasswordChangeRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return nil, h.service.ChangePassword(ctx, id, &req)
}

// ResetPassword sets a new password without the old one, and signs the user out everywhere.
func (h *Handler) ResetPassword(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.PasswordResetRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return nil, h.service.ResetPassword(ctx, id, &req)
}

// RevokeSessions signs a user out everywhere.
func (h *Handler) RevokeSessions(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return nil, h.service.RevokeSessions(ctx, id)
}
