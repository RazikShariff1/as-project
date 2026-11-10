// Package customer holds the HTTP handlers for /customer: a customer's own profile under /customer/me, and staff
// management of customers under /customer and /customer/{id}.
package customer

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/customer-svc/common/authctx"
	"github.com/zopping/customer-svc/common/httpx"
	"github.com/zopping/customer-svc/filters"
	"github.com/zopping/customer-svc/models"
	"github.com/zopping/customer-svc/services"
)

// Handler serves the customer endpoints.
type Handler struct {
	auth     services.Auth
	customer services.Customer
}

// New returns a customer handler.
func New(auth services.Auth, customer services.Customer) *Handler {
	return &Handler{auth: auth, customer: customer}
}

// me resolves the caller from the bearer token. The "me" routes take no id, so a customer cannot name anyone else.
func (h *Handler) me(ctx *gofr.Context) (int64, error) {
	p, err := h.auth.Authenticate(ctx, authctx.Token(ctx))

	return p.Customer.ID, err
}

// Me returns the caller's profile.
func (h *Handler) Me(ctx *gofr.Context) (any, error) {
	id, err := h.me(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.customer.Get(ctx, id))
}

// UpdateMe changes the caller's name and phone.
func (h *Handler) UpdateMe(ctx *gofr.Context) (any, error) {
	id, err := h.me(ctx)
	if err != nil {
		return nil, err
	}

	var req models.ProfileRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.customer.UpdateProfile(ctx, id, &req))
}

// ChangeMyPassword changes the caller's password; every session, this one included, ends.
func (h *Handler) ChangeMyPassword(ctx *gofr.Context) (any, error) {
	id, err := h.me(ctx)
	if err != nil {
		return nil, err
	}

	var req models.PasswordChangeRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return nil, h.customer.ChangePassword(ctx, id, &req)
}

// Index lists customers (staff).
func (h *Handler) Index(ctx *gofr.Context) (any, error) {
	limit, offset, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	f := &filters.CustomerFilter{
		Name: ctx.Param("name"), Email: ctx.Param("email"), Phone: ctx.Param("phone"), Status: ctx.Param("status"),
		Limit: limit, Offset: offset,
	}

	return httpx.Respond(h.customer.List(ctx, f))
}

// Read returns one customer (staff).
func (h *Handler) Read(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.customer.Get(ctx, id))
}

// UpdateStatus suspends or reactivates a customer (staff).
func (h *Handler) UpdateStatus(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.StatusRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.customer.SetStatus(ctx, id, &req))
}
