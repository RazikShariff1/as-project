// Package sellerrequest holds the HTTP handlers for /seller-product-request.
package sellerrequest

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/httpx"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/services"
)

// Handler serves the seller product request endpoints.
type Handler struct {
	service services.SellerRequest
}

// New returns a seller request handler.
func New(service services.SellerRequest) *Handler { return &Handler{service: service} }

// Index lists requests. Query: sellerId, productId, status, limit, page, offset.
func (h *Handler) Index(ctx *gofr.Context) (any, error) {
	limit, offset, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	sellerID, err := httpx.QueryID(ctx, "sellerId")
	if err != nil {
		return nil, err
	}

	productID, err := httpx.QueryID(ctx, "productId")
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.List(ctx, &filters.SellerProductRequestFilter{
		SellerID: sellerID, ProductID: productID, Status: ctx.Param("status"), Limit: limit, Offset: offset,
	}))
}

// Read returns one request with its items.
func (h *Handler) Read(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.Get(ctx, id))
}

// Create files a request.
func (h *Handler) Create(ctx *gofr.Context) (any, error) {
	var in models.SellerProductRequestInput
	if err := ctx.Bind(&in); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Create(ctx, &in))
}

// Update revises a DRAFT or REJECTED request and resubmits it.
func (h *Handler) Update(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var in models.SellerProductRequestInput
	if err := ctx.Bind(&in); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Update(ctx, id, &in))
}

// Delete soft-deletes a request that is not approved.
func (h *Handler) Delete(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return nil, h.service.Delete(ctx, id)
}

// Approval approves or rejects a request (admin). Repeating an approval re-sends the change event.
func (h *Handler) Approval(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.ApprovalRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Approval(ctx, id, &req))
}

// Visibility shows or hides an approved request's live offer.
func (h *Handler) Visibility(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.VisibilityRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Visibility(ctx, id, &req))
}
