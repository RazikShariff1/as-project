// Package address holds the HTTP handlers for /customer/me/address. The customer is always the token's owner.
package address

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/customer-svc/common/authctx"
	"github.com/zopping/customer-svc/common/httpx"
	"github.com/zopping/customer-svc/models"
	"github.com/zopping/customer-svc/services"
)

// Handler serves the address endpoints.
type Handler struct {
	auth    services.Auth
	address services.Address
}

// New returns an address handler.
func New(auth services.Auth, address services.Address) *Handler {
	return &Handler{auth: auth, address: address}
}

func (h *Handler) me(ctx *gofr.Context) (int64, error) {
	p, err := h.auth.Authenticate(ctx, authctx.Token(ctx))

	return p.Customer.ID, err
}

// Index lists the caller's addresses.
func (h *Handler) Index(ctx *gofr.Context) (any, error) {
	cid, err := h.me(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.address.List(ctx, cid))
}

// Read returns one of the caller's addresses.
func (h *Handler) Read(ctx *gofr.Context) (any, error) {
	cid, id, err := h.ids(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.address.Get(ctx, cid, id))
}

// Create saves an address for the caller.
func (h *Handler) Create(ctx *gofr.Context) (any, error) {
	cid, err := h.me(ctx)
	if err != nil {
		return nil, err
	}

	var req models.AddressRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.address.Create(ctx, cid, &req))
}

// Update replaces one of the caller's addresses.
func (h *Handler) Update(ctx *gofr.Context) (any, error) {
	cid, id, err := h.ids(ctx)
	if err != nil {
		return nil, err
	}

	var req models.AddressRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.address.Update(ctx, cid, id, &req))
}

// Delete removes one of the caller's addresses.
func (h *Handler) Delete(ctx *gofr.Context) (any, error) {
	cid, id, err := h.ids(ctx)
	if err != nil {
		return nil, err
	}

	return nil, h.address.Delete(ctx, cid, id)
}

func (h *Handler) ids(ctx *gofr.Context) (customerID, id int64, err error) {
	if customerID, err = h.me(ctx); err != nil {
		return 0, 0, err
	}

	id, err = httpx.PathID(ctx)

	return customerID, id, err
}
