// Package product holds the business rules for products.
package product

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

var _ services.Product = (*Service)(nil)

// Service implements services.Product.
type Service struct {
	products stores.Product
	variants stores.Variant
}

// New returns a product service.
func New(products stores.Product, variants stores.Variant) *Service {
	return &Service{products: products, variants: variants}
}

// normalise validates the editable fields, generates the slug, defaults the status to ENABLED and makes
// CategoryIDs a de-duplicated set that always includes the primary category.
func normalise(req *models.ProductRequest) error {
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

	ids, err := categorySet(req.PrimaryCategoryID, req.CategoryIDs)
	if err != nil {
		return err
	}

	req.CategoryIDs = ids

	return nil
}

func categorySet(primary *int64, extra []int64) ([]int64, error) {
	seen := make(map[int64]bool, len(extra)+1)
	out := make([]int64, 0, len(extra)+1)

	add := func(id int64) error {
		if id < 1 {
			return httpx.InvalidParam("categoryIds")
		}

		if !seen[id] {
			seen[id] = true

			out = append(out, id)
		}

		return nil
	}

	if primary != nil {
		if err := add(*primary); err != nil {
			return nil, httpx.InvalidParam("primaryCategoryId")
		}
	}

	for _, id := range extra {
		if err := add(id); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// Create validates and creates a product. Products are shared content; sellers offer them through inventory-svc.
func (s *Service) Create(ctx *gofr.Context, req *models.ProductRequest) (models.Product, error) {
	if err := normalise(req); err != nil {
		return models.Product{}, err
	}

	p, err := s.products.Create(ctx, req)
	if err != nil {
		return models.Product{}, httpx.FromStore(err, "product", req.Slug)
	}

	return p, nil
}

// Get returns one product with its variants.
func (s *Service) Get(ctx *gofr.Context, id int64) (models.Product, error) {
	p, err := s.products.Get(ctx, id)
	if err != nil {
		return models.Product{}, httpx.FromStoreID(err, "id", id)
	}

	return s.withVariants(ctx, p)
}

// List returns one page of products, each with its variants.
func (s *Service) List(ctx *gofr.Context, f *filters.ProductFilter) (models.ProductList, error) {
	if f.Status != "" && !models.ValidStatus(f.Status) {
		return models.ProductList{}, httpx.InvalidParam("status")
	}

	products, count, err := s.products.List(ctx, f)
	if err != nil {
		return models.ProductList{}, httpx.FromStore(err, "filter", "")
	}

	if err := s.attachVariants(ctx, products); err != nil {
		return models.ProductList{}, err
	}

	return models.ProductList{Products: products, Count: count, Offset: f.Offset, Limit: f.Limit}, nil
}

// Update validates and replaces a product's editable fields.
func (s *Service) Update(ctx *gofr.Context, id int64, req *models.ProductRequest) (models.Product, error) {
	if err := normalise(req); err != nil {
		return models.Product{}, err
	}

	p, err := s.products.Update(ctx, id, req)
	if err != nil {
		return models.Product{}, httpx.FromStoreID(err, "id", id)
	}

	return s.withVariants(ctx, p)
}

// Delete soft-deletes a product and its variants; the rows stay so past orders keep resolving.
func (s *Service) Delete(ctx *gofr.Context, id int64) error {
	if err := s.products.Delete(ctx, id); err != nil {
		return httpx.FromStoreID(err, "id", id)
	}

	return nil
}

// withVariants returns p with its variants loaded.
func (s *Service) withVariants(ctx *gofr.Context, p models.Product) (models.Product, error) {
	ps := []models.Product{p}
	if err := s.attachVariants(ctx, ps); err != nil {
		return models.Product{}, err
	}

	return ps[0], nil
}

// attachVariants loads the variants of the given products in one query and sets them in place.
func (s *Service) attachVariants(ctx *gofr.Context, products []models.Product) error {
	if len(products) == 0 {
		return nil
	}

	ids := make([]int64, len(products))
	index := make(map[int64]int, len(products))

	for i := range products {
		ids[i] = products[i].ID
		index[products[i].ID] = i
	}

	variants, err := s.variants.ListByProductIDs(ctx, ids)
	if err != nil {
		return err
	}

	for i := range variants {
		p := &products[index[variants[i].ProductID]]
		p.Variants = append(p.Variants, variants[i])
	}

	return nil
}
