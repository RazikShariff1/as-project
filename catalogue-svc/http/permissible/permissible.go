// Package permissible holds the HTTP handlers for the per-product whitelist of sellers.
package permissible

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/httpx"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/services"
)

// Handler serves the permissible-seller endpoints.
type Handler struct {
	service services.PermissibleSeller
}

// New returns a permissible-seller handler.
func New(service services.PermissibleSeller) *Handler { return &Handler{service: service} }

// Index lists permissible sellers. Query: sellerId, productId, status, limit, page, offset.
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

	return httpx.Respond(h.service.List(ctx, &filters.PermissibleSellerFilter{
		SellerID: sellerID, ProductID: productID, Status: ctx.Param("status"), Limit: limit, Offset: offset,
	}))
}

// Replace sets the sellers allowed to sell product {id}: the listed ones are ENABLED, the others DISABLED.
func (h *Handler) Replace(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.PermissibleSellersRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Replace(ctx, id, &req))
}
