// Package stock holds the HTTP handlers for /stock. Every offer-level path is /stock/{id}/seller/{sellerId}, where {id}
// is the catalogue variant id.
package stock

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/inventory-svc/common/apperr"
	"github.com/zopping/inventory-svc/common/caller"
	"github.com/zopping/inventory-svc/common/httpx"
	"github.com/zopping/inventory-svc/filters"
	"github.com/zopping/inventory-svc/models"
	"github.com/zopping/inventory-svc/services"
)

// Handler serves the stock endpoints.
type Handler struct {
	service services.Stock
}

// New returns a stock handler.
func New(service services.Stock) *Handler { return &Handler{service: service} }

// ids reads the variant and seller from the path. A caller the gateway restricted to one seller (see common/caller) may only
// name that seller; the gateway checks this too, and this is the backstop if a request ever reaches the service another way.
func ids(ctx *gofr.Context) (variantID, sellerID int64, err error) {
	if variantID, err = httpx.PathID(ctx); err != nil {
		return 0, 0, err
	}

	if sellerID, err = httpx.PathSellerID(ctx); err != nil {
		return 0, 0, err
	}

	if own, restricted := caller.Seller(ctx); restricted && own != sellerID {
		return 0, 0, &apperr.Forbidden{Reason: "you can only act for your own seller"}
	}

	return variantID, sellerID, nil
}

// Index lists offers. Query: variantId, sellerId, status, lowStock, outOfStock, limit, page, offset.
func (h *Handler) Index(ctx *gofr.Context) (any, error) {
	limit, offset, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	variantID, err := httpx.QueryID(ctx, "variantId")
	if err != nil {
		return nil, err
	}

	sellerID, err := httpx.QueryID(ctx, "sellerId")
	if err != nil {
		return nil, err
	}

	if own, restricted := caller.Seller(ctx); restricted {
		if sellerID != 0 && sellerID != own {
			return nil, &apperr.Forbidden{Reason: "you can only act for your own seller"}
		}

		sellerID = own // a seller lists only their own stock
	}

	low, err := httpx.QueryBool(ctx, "lowStock")
	if err != nil {
		return nil, err
	}

	out, err := httpx.QueryBool(ctx, "outOfStock")
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.List(ctx, &filters.StockFilter{
		VariantID: variantID, SellerID: sellerID, Status: ctx.Param("status"), LowStock: low, OutOfStock: out,
		Limit: limit, Offset: offset,
	}))
}

// Read returns one seller's offer for a variant.
func (h *Handler) Read(ctx *gofr.Context) (any, error) {
	variantID, sellerID, err := ids(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.Get(ctx, variantID, sellerID))
}

// Put sets a seller's stock of a variant to an absolute quantity, creating it on first use.
func (h *Handler) Put(ctx *gofr.Context) (any, error) {
	variantID, sellerID, err := ids(ctx)
	if err != nil {
		return nil, err
	}

	var req models.StockRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Put(ctx, variantID, sellerID, &req))
}

// Adjust changes a seller's quantity by a delta.
func (h *Handler) Adjust(ctx *gofr.Context) (any, error) {
	variantID, sellerID, err := ids(ctx)
	if err != nil {
		return nil, err
	}

	var req models.AdjustRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Adjust(ctx, variantID, sellerID, &req))
}

// SetPrice replaces a seller's mrp, discount, cost price, tax and HSN code. The offer must already exist.
func (h *Handler) SetPrice(ctx *gofr.Context) (any, error) {
	variantID, sellerID, err := ids(ctx)
	if err != nil {
		return nil, err
	}

	var req models.PriceRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.SetPrice(ctx, variantID, sellerID, &req))
}

// UpsertOffer creates a seller's offer or updates its mrp and discount, never touching quantity. Repeating it is safe;
// it is what an approved seller product request calls.
func (h *Handler) UpsertOffer(ctx *gofr.Context) (any, error) {
	variantID, sellerID, err := ids(ctx)
	if err != nil {
		return nil, err
	}

	var req models.OfferRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.UpsertOffer(ctx, variantID, sellerID, &req))
}

// Withdraw hides a seller's offer; stock, history and existing reservations are kept.
func (h *Handler) Withdraw(ctx *gofr.Context) (any, error) {
	variantID, sellerID, err := ids(ctx)
	if err != nil {
		return nil, err
	}

	return nil, h.service.Withdraw(ctx, variantID, sellerID)
}

// PriceLog returns an offer's price-change history, newest first.
func (h *Handler) PriceLog(ctx *gofr.Context) (any, error) {
	variantID, sellerID, err := ids(ctx)
	if err != nil {
		return nil, err
	}

	limit, offset, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.PriceLog(ctx, &filters.PriceLogFilter{
		VariantID: variantID, SellerID: sellerID, Limit: limit, Offset: offset,
	}))
}

// Availability returns the storefront's view of many variants: every visible seller's offer, cheapest first, and the
// default one. Query: variantIds=1,2,3 (at most 100).
func (h *Handler) Availability(ctx *gofr.Context) (any, error) {
	variantIDs, err := httpx.QueryIDs(ctx, "variantIds")
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.Availability(ctx, variantIDs))
}

// Movements returns an offer's quantity history, newest first.
func (h *Handler) Movements(ctx *gofr.Context) (any, error) {
	variantID, sellerID, err := ids(ctx)
	if err != nil {
		return nil, err
	}

	limit, offset, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.Movements(ctx, &filters.MovementFilter{
		VariantID: variantID, SellerID: sellerID, Limit: limit, Offset: offset,
	}))
}
