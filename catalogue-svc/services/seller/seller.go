// Package seller holds the business rules for sellers.
package seller

import (
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/httpx"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/services"
	"github.com/zopping/catalogue-svc/stores"
)

var _ services.Seller = (*Service)(nil)

// Service implements services.Seller.
type Service struct {
	store stores.Seller
}

// New returns a seller service.
func New(store stores.Seller) *Service { return &Service{store: store} }

func validStatus(s string) bool {
	return s == models.SellerStatusActive || s == models.SellerStatusSuspended
}

// Create validates and creates a seller.
func (s *Service) Create(ctx *gofr.Context, req *models.SellerRequest) (models.Seller, error) {
	if strings.TrimSpace(req.Name) == "" {
		return models.Seller{}, httpx.InvalidParam("name")
	}

	seller, err := s.store.Create(ctx, req)
	if err != nil {
		return models.Seller{}, httpx.FromStore(err, "seller", req.Name)
	}

	return seller, nil
}

// Get returns one seller.
func (s *Service) Get(ctx *gofr.Context, id int64) (models.Seller, error) {
	seller, err := s.store.Get(ctx, id)
	if err != nil {
		return models.Seller{}, httpx.FromStoreID(err, "id", id)
	}

	return seller, nil
}

// List returns sellers matching the filter.
func (s *Service) List(ctx *gofr.Context, f *filters.SellerFilter) ([]models.Seller, error) {
	if f.Status != "" && !validStatus(f.Status) {
		return nil, httpx.InvalidParam("status")
	}

	return s.store.List(ctx, f)
}

// Update validates and replaces a seller's name and status.
func (s *Service) Update(ctx *gofr.Context, id int64, req *models.SellerRequest) (models.Seller, error) {
	if strings.TrimSpace(req.Name) == "" {
		return models.Seller{}, httpx.InvalidParam("name")
	}

	if !validStatus(req.Status) {
		return models.Seller{}, httpx.InvalidParam("status")
	}

	seller, err := s.store.Update(ctx, id, req)
	if err != nil {
		return models.Seller{}, httpx.FromStoreID(err, "id", id)
	}

	return seller, nil
}
