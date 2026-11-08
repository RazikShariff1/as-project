// Package variant holds the business rules for variants.
package variant

import (
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/httpx"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/services"
	"github.com/zopping/catalogue-svc/stores"
)

var _ services.Variant = (*Service)(nil)

// Service implements services.Variant.
type Service struct {
	store stores.Variant
}

// New returns a variant service.
func New(store stores.Variant) *Service { return &Service{store: store} }

// normalise validates the editable fields and defaults the status to ENABLED.
func normalise(req *models.VariantRequest) error {
	if strings.TrimSpace(req.ClientItemID) == "" {
		return httpx.InvalidParam("clientItemId")
	}

	if req.Status == "" {
		req.Status = models.StatusEnabled
	}

	if !models.ValidStatus(req.Status) {
		return httpx.InvalidParam("status")
	}

	return nil
}

// Create validates and creates a variant under an existing product.
func (s *Service) Create(ctx *gofr.Context, req *models.VariantRequest) (models.Variant, error) {
	if req.ProductID < 1 {
		return models.Variant{}, httpx.InvalidParam("productId")
	}

	if err := normalise(req); err != nil {
		return models.Variant{}, err
	}

	v, err := s.store.Create(ctx, req)
	if err != nil {
		return models.Variant{}, httpx.FromStoreID(err, "productId", req.ProductID)
	}

	return v, nil
}

// Get returns one variant.
func (s *Service) Get(ctx *gofr.Context, id int64) (models.Variant, error) {
	v, err := s.store.Get(ctx, id)
	if err != nil {
		return models.Variant{}, httpx.FromStoreID(err, "id", id)
	}

	return v, nil
}

// List returns one page of variants.
func (s *Service) List(ctx *gofr.Context, f *filters.VariantFilter) (models.VariantList, error) {
	if f.Status != "" && !models.ValidStatus(f.Status) {
		return models.VariantList{}, httpx.InvalidParam("status")
	}

	variants, count, err := s.store.List(ctx, f)
	if err != nil {
		return models.VariantList{}, httpx.FromStore(err, "filter", "")
	}

	return models.VariantList{Variants: variants, Count: count, Offset: f.Offset, Limit: f.Limit}, nil
}

// Update validates and replaces a variant's editable fields.
func (s *Service) Update(ctx *gofr.Context, id int64, req *models.VariantRequest) (models.Variant, error) {
	if err := normalise(req); err != nil {
		return models.Variant{}, err
	}

	v, err := s.store.Update(ctx, id, req)
	if err != nil {
		return models.Variant{}, httpx.FromStoreID(err, "id", id)
	}

	return v, nil
}

// Delete soft-deletes a variant; the row stays so past orders keep resolving.
func (s *Service) Delete(ctx *gofr.Context, id int64) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return httpx.FromStoreID(err, "id", id)
	}

	return nil
}
