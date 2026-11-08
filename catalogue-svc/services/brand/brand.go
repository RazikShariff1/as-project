// Package brand holds the business rules for brands.
package brand

import (
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/httpx"
	"github.com/zopping/catalogue-svc/common/slug"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/services"
	"github.com/zopping/catalogue-svc/stores"
)

var _ services.Brand = (*Service)(nil)

// Service implements services.Brand.
type Service struct {
	store stores.Brand
}

// New returns a brand service.
func New(store stores.Brand) *Service { return &Service{store: store} }

// normalise validates the request, generating the slug and defaulting the status.
func normalise(req *models.BrandRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return httpx.InvalidParam("name")
	}

	req.Slug = slug.Resolve(req.Slug, req.Name)
	if !slug.Valid(req.Slug) {
		return httpx.InvalidParam("slug")
	}

	if req.Status == "" {
		req.Status = models.StatusEnabled
	}

	if !models.ValidStatus(req.Status) {
		return httpx.InvalidParam("status")
	}

	return nil
}

// Create validates and creates a brand.
func (s *Service) Create(ctx *gofr.Context, req *models.BrandRequest) (models.Brand, error) {
	if err := normalise(req); err != nil {
		return models.Brand{}, err
	}

	b, err := s.store.Create(ctx, req)
	if err != nil {
		return models.Brand{}, httpx.FromStore(err, "brand", req.Slug)
	}

	return b, nil
}

// Get returns one brand.
func (s *Service) Get(ctx *gofr.Context, id int64) (models.Brand, error) {
	b, err := s.store.Get(ctx, id)
	if err != nil {
		return models.Brand{}, httpx.FromStoreID(err, "id", id)
	}

	return b, nil
}

// List returns one page of brands.
func (s *Service) List(ctx *gofr.Context, f *filters.BrandFilter) (models.BrandList, error) {
	if f.Status != "" && !models.ValidStatus(f.Status) {
		return models.BrandList{}, httpx.InvalidParam("status")
	}

	brands, count, err := s.store.List(ctx, f)
	if err != nil {
		return models.BrandList{}, httpx.FromStore(err, "filter", "")
	}

	return models.BrandList{Brands: brands, Count: count, Offset: f.Offset, Limit: f.Limit}, nil
}

// Update validates and replaces a brand's editable fields.
func (s *Service) Update(ctx *gofr.Context, id int64, req *models.BrandRequest) (models.Brand, error) {
	if err := normalise(req); err != nil {
		return models.Brand{}, err
	}

	b, err := s.store.Update(ctx, id, req)
	if err != nil {
		return models.Brand{}, httpx.FromStoreID(err, "id", id)
	}

	return b, nil
}

// Delete soft-deletes a brand.
func (s *Service) Delete(ctx *gofr.Context, id int64) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return httpx.FromStoreID(err, "id", id)
	}

	return nil
}
