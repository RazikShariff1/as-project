// Package services declares the business-logic contracts; implementations live in one sub-package per domain.
package services

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/inventory-svc/filters"
	"github.com/zopping/inventory-svc/models"
)

// Stock is the business-logic contract for sellers' offers (stock and price). Every call names the variant and the seller.
type Stock interface {
	Get(ctx *gofr.Context, variantID, sellerID int64) (models.Stock, error)
	List(ctx *gofr.Context, f *filters.StockFilter) (models.StockList, error)
	Put(ctx *gofr.Context, variantID, sellerID int64, req *models.StockRequest) (models.Stock, error)
	Adjust(ctx *gofr.Context, variantID, sellerID int64, req *models.AdjustRequest) (models.Stock, error)
	SetPrice(ctx *gofr.Context, variantID, sellerID int64, req *models.PriceRequest) (models.Stock, error)
	// UpsertOffer is called when a seller product request is approved: it creates the offer or updates its mrp and
	// discount, never touching quantity.
	UpsertOffer(ctx *gofr.Context, variantID, sellerID int64, req *models.OfferRequest) (models.Stock, error)
	// Withdraw hides a seller's offer.
	Withdraw(ctx *gofr.Context, variantID, sellerID int64) error
	PriceLog(ctx *gofr.Context, f *filters.PriceLogFilter) (models.PriceChangeList, error)
	// Availability returns every visible seller's offer per variant, cheapest first, with the default one flagged.
	Availability(ctx *gofr.Context, variantIDs []int64) ([]models.VariantAvailability, error)
	Movements(ctx *gofr.Context, f *filters.MovementFilter) (models.MovementList, error)
}

// Reservation is the business-logic contract for reservations.
type Reservation interface {
	Create(ctx *gofr.Context, req *models.ReservationRequest) (models.Reservation, error)
	Get(ctx *gofr.Context, orderRef string) (models.Reservation, error)
	Confirm(ctx *gofr.Context, orderRef string) (models.Reservation, error)
	Release(ctx *gofr.Context, orderRef string) (models.Reservation, error)
	// ReleaseExpired releases up to limit orders whose hold has expired and returns how many it released.
	ReleaseExpired(ctx *gofr.Context, limit int) (int, error)
}
