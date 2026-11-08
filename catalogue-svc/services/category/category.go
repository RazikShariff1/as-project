// Package category holds the business rules for categories.
package category

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

var _ services.Category = (*Service)(nil)

// Service implements services.Category.
type Service struct {
	store stores.Category
}

// New returns a category service.
func New(store stores.Category) *Service { return &Service{store: store} }

// normalise validates the request, generating the slug and defaulting the status.
func normalise(req *models.CategoryRequest) error {
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

// Create validates and creates a category.
func (s *Service) Create(ctx *gofr.Context, req *models.CategoryRequest) (models.Category, error) {
	if err := normalise(req); err != nil {
		return models.Category{}, err
	}

	c, err := s.store.Create(ctx, req)
	if err != nil {
		return models.Category{}, httpx.FromStore(err, "parentCategoryId", "")
	}

	return c, nil
}

// Get returns one category.
func (s *Service) Get(ctx *gofr.Context, id int64) (models.Category, error) {
	c, err := s.store.Get(ctx, id)
	if err != nil {
		return models.Category{}, httpx.FromStoreID(err, "id", id)
	}

	return c, nil
}

// List returns one page of categories.
func (s *Service) List(ctx *gofr.Context, f *filters.CategoryFilter) (models.CategoryList, error) {
	if f.Status != "" && !models.ValidStatus(f.Status) {
		return models.CategoryList{}, httpx.InvalidParam("status")
	}

	categories, count, err := s.store.List(ctx, f)
	if err != nil {
		return models.CategoryList{}, httpx.FromStore(err, "filter", "")
	}

	return models.CategoryList{Categories: categories, Count: count, Offset: f.Offset, Limit: f.Limit}, nil
}

// Update validates and replaces a category. A category cannot be its own parent.
func (s *Service) Update(ctx *gofr.Context, id int64, req *models.CategoryRequest) (models.Category, error) {
	if err := normalise(req); err != nil {
		return models.Category{}, err
	}

	if req.ParentCategoryID != nil && *req.ParentCategoryID == id {
		return models.Category{}, httpx.InvalidParam("parentCategoryId")
	}

	c, err := s.store.Update(ctx, id, req)
	if err != nil {
		return models.Category{}, httpx.FromStoreID(err, "id", id)
	}

	return c, nil
}

// Delete soft-deletes a category.
func (s *Service) Delete(ctx *gofr.Context, id int64) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return httpx.FromStoreID(err, "id", id)
	}

	return nil
}
