// Package permissible holds the business rules for the per-product whitelist of sellers.
package permissible

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/apperr"
	"github.com/zopping/catalogue-svc/common/caller"
	"github.com/zopping/catalogue-svc/common/httpx"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/services"
	"github.com/zopping/catalogue-svc/stores"
)

const listAll = 1000

var _ services.PermissibleSeller = (*Service)(nil)

// Service implements services.PermissibleSeller.
type Service struct {
	store    stores.PermissibleSeller
	products stores.Product
}

// New returns a permissible-seller service.
func New(store stores.PermissibleSeller, products stores.Product) *Service {
	return &Service{store: store, products: products}
}

// Replace sets the sellers allowed to sell a product and returns the product's whole list afterwards.
func (s *Service) Replace(ctx *gofr.Context, productID int64, req *models.PermissibleSellersRequest) ([]models.PermissibleSeller, error) {
	if _, restricted := caller.Seller(ctx); restricted {
		return nil, &apperr.Forbidden{Reason: "only an administrator can change who may sell a product"}
	}

	seen := make(map[int64]bool, len(req.SellerIDs))
	ids := make([]int64, 0, len(req.SellerIDs))

	for _, id := range req.SellerIDs {
		if id < 1 {
			return nil, httpx.InvalidParam("sellerIds")
		}

		if !seen[id] {
			seen[id] = true

			ids = append(ids, id)
		}
	}

	if _, err := s.products.Get(ctx, productID); err != nil {
		return nil, httpx.FromStoreID(err, "id", productID)
	}

	if err := s.store.Replace(ctx, productID, ids); err != nil {
		return nil, httpx.FromStoreID(err, "id", productID)
	}

	items, _, err := s.store.List(ctx, &filters.PermissibleSellerFilter{ProductID: productID, Limit: listAll})
	if err != nil {
		return nil, httpx.FromStore(err, "filter", "")
	}

	return items, nil
}

// List returns one page of permissible sellers.
func (s *Service) List(ctx *gofr.Context, f *filters.PermissibleSellerFilter) (models.PermissibleSellerList, error) {
	if f.Status != "" && f.Status != models.PermissibleEnabled && f.Status != models.PermissibleDisabled {
		return models.PermissibleSellerList{}, httpx.InvalidParam("status")
	}

	if id, restricted := caller.Seller(ctx); restricted {
		f.SellerID = id // a seller sees only their own permissions
	}

	items, count, err := s.store.List(ctx, f)
	if err != nil {
		return models.PermissibleSellerList{}, httpx.FromStore(err, "filter", "")
	}

	return models.PermissibleSellerList{PermissibleSellers: items, Count: count, Offset: f.Offset, Limit: f.Limit}, nil
}
