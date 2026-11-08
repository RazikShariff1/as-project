// Package stock holds the business rules for sellers' offers: stock levels and prices.
package stock

import (
	"sort"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/inventory-svc/common/httpx"
	"github.com/zopping/inventory-svc/filters"
	"github.com/zopping/inventory-svc/models"
	"github.com/zopping/inventory-svc/services"
	"github.com/zopping/inventory-svc/stores"
)

var _ services.Stock = (*Service)(nil)

// Service implements services.Stock.
type Service struct {
	store         stores.Stock
	houseSellerID int64
}

// New returns a stock service. houseSellerID (0 for none) is the seller that wins a price tie, so our own offer is
// shown first when prices match.
func New(store stores.Stock, houseSellerID int64) *Service {
	return &Service{store: store, houseSellerID: houseSellerID}
}

// Get returns one seller's offer for a variant.
func (s *Service) Get(ctx *gofr.Context, variantID, sellerID int64) (models.Stock, error) {
	st, err := s.store.Get(ctx, variantID, sellerID)
	if err != nil {
		return models.Stock{}, httpx.FromStoreID(err, "id", variantID)
	}

	return st, nil
}

// List returns one page of stock rows.
func (s *Service) List(ctx *gofr.Context, f *filters.StockFilter) (models.StockList, error) {
	if f.Status != "" && !models.ValidStatus(f.Status) {
		return models.StockList{}, httpx.InvalidParam("status")
	}

	stock, count, err := s.store.List(ctx, f)
	if err != nil {
		return models.StockList{}, httpx.FromStore(err, "filter", "")
	}

	return models.StockList{Stock: stock, Count: count, Offset: f.Offset, Limit: f.Limit}, nil
}

// Put validates and sets a seller's stock of an existing offer to an absolute quantity.
func (s *Service) Put(ctx *gofr.Context, variantID, sellerID int64, req *models.StockRequest) (models.Stock, error) {
	if req.Quantity < 0 {
		return models.Stock{}, httpx.InvalidParam("quantity")
	}

	if req.LowStockThreshold != nil && *req.LowStockThreshold < 0 {
		return models.Stock{}, httpx.InvalidParam("lowStockThreshold")
	}

	if req.Unit == 0 {
		req.Unit = 1
	}

	if req.Unit < 1 {
		return models.Stock{}, httpx.InvalidParam("unit")
	}

	if req.UserID != nil && *req.UserID < 1 {
		return models.Stock{}, httpx.InvalidParam("userId")
	}

	st, err := s.store.Put(ctx, variantID, sellerID, req)
	if err != nil {
		return models.Stock{}, httpx.FromStoreID(err, "id", variantID)
	}

	return st, nil
}

// Adjust validates and applies a quantity change. The reason must be a known one and defaults to OTHER. A reference
// identifies the business document behind the change and makes it idempotent, so it needs both a type and an id.
func (s *Service) Adjust(ctx *gofr.Context, variantID, sellerID int64, req *models.AdjustRequest) (models.Stock, error) {
	if req.Delta == 0 {
		return models.Stock{}, httpx.InvalidParam("delta")
	}

	req.Reason = strings.ToUpper(strings.TrimSpace(req.Reason))
	if req.Reason == "" {
		req.Reason = models.ReasonOther
	}

	if !models.ValidReason(req.Reason) {
		return models.Stock{}, httpx.InvalidParam("reason")
	}

	if req.UserID != nil && *req.UserID < 1 {
		return models.Stock{}, httpx.InvalidParam("userId")
	}

	if (req.ReferenceType == "") != (req.ReferenceID == "") {
		return models.Stock{}, httpx.InvalidParam("referenceType", "referenceId")
	}

	st, err := s.store.Adjust(ctx, variantID, sellerID, req)
	if err != nil {
		return models.Stock{}, httpx.FromStoreID(err, "id", variantID)
	}

	return st, nil
}

// SetPrice validates and replaces a seller's price, discount, cost price, tax and HSN code for a variant. Amounts are
// rupees and cannot be negative; a discount is an amount off the MRP, so it needs an MRP and cannot exceed it.
func (s *Service) SetPrice(ctx *gofr.Context, variantID, sellerID int64, req *models.PriceRequest) (models.Stock, error) {
	switch {
	case req.MRP != nil && *req.MRP < 0:
		return models.Stock{}, httpx.InvalidParam("mrp")
	case req.Discount < 0:
		return models.Stock{}, httpx.InvalidParam("discount")
	case req.Discount > 0 && req.MRP == nil:
		return models.Stock{}, httpx.InvalidParam("mrp")
	case req.MRP != nil && req.Discount > *req.MRP:
		return models.Stock{}, httpx.InvalidParam("discount")
	case req.CostPrice != nil && *req.CostPrice < 0:
		return models.Stock{}, httpx.InvalidParam("costPrice")
	case req.UserID != nil && *req.UserID < 1:
		return models.Stock{}, httpx.InvalidParam("userId")
	}

	req.Tax, req.HSNCode = strings.TrimSpace(req.Tax), strings.TrimSpace(req.HSNCode)

	st, err := s.store.SetPrice(ctx, variantID, sellerID, req)
	if err != nil {
		return models.Stock{}, httpx.FromStoreID(err, "id", variantID)
	}

	return st, nil
}

// UpsertOffer validates and creates or updates a seller's offer from an approved request. Same price rules as SetPrice.
func (s *Service) UpsertOffer(ctx *gofr.Context, variantID, sellerID int64, req *models.OfferRequest) (models.Stock, error) {
	switch {
	case req.MRP < 0:
		return models.Stock{}, httpx.InvalidParam("mrp")
	case req.Discount < 0 || req.Discount > req.MRP:
		return models.Stock{}, httpx.InvalidParam("discount")
	case req.UserID != nil && *req.UserID < 1:
		return models.Stock{}, httpx.InvalidParam("userId")
	}

	st, err := s.store.UpsertOffer(ctx, variantID, sellerID, req)
	if err != nil {
		return models.Stock{}, httpx.FromStoreID(err, "id", variantID)
	}

	return st, nil
}

// Withdraw hides a seller's offer; history and existing reservations are kept.
func (s *Service) Withdraw(ctx *gofr.Context, variantID, sellerID int64) error {
	if err := s.store.Withdraw(ctx, variantID, sellerID); err != nil {
		return httpx.FromStoreID(err, "id", variantID)
	}

	return nil
}

// PriceLog returns one page of an offer's price-change history.
func (s *Service) PriceLog(ctx *gofr.Context, f *filters.PriceLogFilter) (models.PriceChangeList, error) {
	changes, count, err := s.store.PriceLog(ctx, f)
	if err != nil {
		return models.PriceChangeList{}, httpx.FromStore(err, "filter", "")
	}

	return models.PriceChangeList{Changes: changes, Count: count, Offset: f.Offset, Limit: f.Limit}, nil
}

// Movements returns one page of an offer's movement history.
func (s *Service) Movements(ctx *gofr.Context, f *filters.MovementFilter) (models.MovementList, error) {
	movements, count, err := s.store.Movements(ctx, f)
	if err != nil {
		return models.MovementList{}, httpx.FromStore(err, "filter", "")
	}

	return models.MovementList{Movements: movements, Count: count, Offset: f.Offset, Limit: f.Limit}, nil
}

// Availability returns each variant's visible offers cheapest first, with the default offer flagged.
func (s *Service) Availability(ctx *gofr.Context, variantIDs []int64) ([]models.VariantAvailability, error) {
	if len(variantIDs) == 0 {
		return nil, httpx.InvalidParam("variantIds")
	}

	out, err := s.store.Offers(ctx, variantIDs)
	if err != nil {
		return nil, httpx.FromStore(err, "variantIds", "")
	}

	for i := range out {
		s.rank(&out[i])
	}

	return out, nil
}

// rank orders a variant's offers cheapest first (offers without a price last) and picks the default: the lowest
// selling price among offers with stock. A price tie goes to the house seller, then to the larger quantity, then to the
// lower seller id, so the choice is always the same for the same data.
func (s *Service) rank(v *models.VariantAvailability) {
	sort.SliceStable(v.Offers, func(i, j int) bool {
		a, b := v.Offers[i], v.Offers[j]

		switch {
		case a.SellingPrice == nil && b.SellingPrice == nil:
			return a.SellerID < b.SellerID
		case a.SellingPrice == nil:
			return false
		case b.SellingPrice == nil:
			return true
		case *a.SellingPrice != *b.SellingPrice:
			return *a.SellingPrice < *b.SellingPrice
		default:
			return s.better(a, b)
		}
	})

	// The offers are now ordered by price and tie-break, so the first one that is in stock and priced is the winner.
	for i := range v.Offers {
		if v.Offers[i].InStock && v.Offers[i].SellingPrice != nil {
			picked := v.Offers[i]
			v.BestOffer = &picked

			break
		}
	}
}

// better reports whether offer a beats offer b at equal price: house seller, then larger stock, then lower seller id.
func (s *Service) better(a, b models.Offer) bool {
	if (a.SellerID == s.houseSellerID) != (b.SellerID == s.houseSellerID) {
		return a.SellerID == s.houseSellerID
	}

	if a.UnlimitedStock != b.UnlimitedStock {
		return a.UnlimitedStock
	}

	if a.Available != b.Available {
		return a.Available > b.Available
	}

	return a.SellerID < b.SellerID
}
