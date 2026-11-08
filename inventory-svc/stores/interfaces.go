// Package stores declares the data-access contracts; implementations live in one sub-package per store.
package stores

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/inventory-svc/filters"
	"github.com/zopping/inventory-svc/models"
)

// Stock is the data-access contract for sellers' offers: stock, price and their audit logs. A row is one seller's offer
// for one variant. Every quantity or price change is written together with its log row in one transaction.
type Stock interface {
	Get(ctx *gofr.Context, variantID, sellerID int64) (models.Stock, error)
	List(ctx *gofr.Context, f *filters.StockFilter) (stock []models.Stock, count int, err error)
	// Put sets the absolute quantity of an offer that already exists (apperr.ErrNotFound otherwise). It never creates a row and
	// never changes status: only the catalogue's approval flow creates, shows and hides offers.
	Put(ctx *gofr.Context, variantID, sellerID int64, req *models.StockRequest) (models.Stock, error)
	// Adjust changes the quantity by a delta. It returns *apperr.InsufficientStock when the result would drop
	// below what is reserved, and apperr.ErrNotFound when the seller has no row for the variant.
	Adjust(ctx *gofr.Context, variantID, sellerID int64, req *models.AdjustRequest) (models.Stock, error)
	// SetPrice replaces mrp, discount, cost price, tax and HSN code with a price-log row per changed field. The row
	// must exist (apperr.ErrNotFound otherwise); quantity is never touched.
	SetPrice(ctx *gofr.Context, variantID, sellerID int64, req *models.PriceRequest) (models.Stock, error)
	// UpsertOffer creates the row with zero stock if missing, or updates only mrp and discount and makes it visible
	// again, logging the price changes. Quantity is never touched, so repeating it is safe.
	UpsertOffer(ctx *gofr.Context, variantID, sellerID int64, req *models.OfferRequest) (models.Stock, error)
	// Withdraw hides the offer (status HIDDEN). Stock, history and existing reservations are kept.
	Withdraw(ctx *gofr.Context, variantID, sellerID int64) error
	// Offers returns the visible (ENABLED) offers of each requested variant, one entry per distinct id in request
	// order; a variant nobody offers has an empty list. Ranking is left to the caller.
	Offers(ctx *gofr.Context, variantIDs []int64) ([]models.VariantAvailability, error)
	Movements(ctx *gofr.Context, f *filters.MovementFilter) (movements []models.Movement, count int, err error)
	PriceLog(ctx *gofr.Context, f *filters.PriceLogFilter) (changes []models.PriceChange, count int, err error)
}

// Reservation is the data-access contract for stock reservations. Each method runs in one transaction and
// is safe to repeat.
type Reservation interface {
	// Create reserves every item or none, holding stock for req.TTLSeconds (computed from the database clock).
	// Items must be sorted by variant then seller so concurrent orders lock rows in the same order. It returns
	// *apperr.InsufficientStock for the first offer that cannot be covered, and *apperr.StateConflict when the order
	// already has a different or released reservation.
	Create(ctx *gofr.Context, req *models.ReservationRequest) (models.Reservation, error)
	Get(ctx *gofr.Context, orderRef string) (models.Reservation, error)
	// Confirm turns reserved stock into a sale: quantity and reserved both drop.
	Confirm(ctx *gofr.Context, orderRef string) (models.Reservation, error)
	// Release gives reserved stock back. A confirmed reservation cannot be released.
	Release(ctx *gofr.Context, orderRef string) (models.Reservation, error)
	// ExpiredOrderRefs lists orders whose reservation has run past its expiry and is still held.
	ExpiredOrderRefs(ctx *gofr.Context, limit int) ([]string, error)
}
