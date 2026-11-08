// Package services declares the business-logic contracts; implementations live in one sub-package per domain.
package services

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
)

// Seller is the business-logic contract for sellers.
type Seller interface {
	Create(ctx *gofr.Context, req *models.SellerRequest) (models.Seller, error)
	Get(ctx *gofr.Context, id int64) (models.Seller, error)
	List(ctx *gofr.Context, f *filters.SellerFilter) ([]models.Seller, error)
	Update(ctx *gofr.Context, id int64, req *models.SellerRequest) (models.Seller, error)
}

// PermissibleSeller is the business-logic contract for the per-product whitelist of sellers.
type PermissibleSeller interface {
	// Replace sets the sellers allowed to sell a product.
	Replace(ctx *gofr.Context, productID int64, req *models.PermissibleSellersRequest) ([]models.PermissibleSeller, error)
	List(ctx *gofr.Context, f *filters.PermissibleSellerFilter) (models.PermissibleSellerList, error)
}

// SellerRequest is the business-logic contract for the seller product request workflow.
type SellerRequest interface {
	Create(ctx *gofr.Context, in *models.SellerProductRequestInput) (models.SellerProductRequest, error)
	Get(ctx *gofr.Context, id int64) (models.SellerProductRequest, error)
	List(ctx *gofr.Context, f *filters.SellerProductRequestFilter) (models.SellerProductRequestList, error)
	Update(ctx *gofr.Context, id int64, in *models.SellerProductRequestInput) (models.SellerProductRequest, error)
	Delete(ctx *gofr.Context, id int64) error
	// Approval approves or rejects a request. Approving publishes the change event; repeating an approval re-sends it.
	Approval(ctx *gofr.Context, id int64, req *models.ApprovalRequest) (models.SellerProductRequest, error)
	// Visibility shows or hides an approved request's live offer and publishes the change event.
	Visibility(ctx *gofr.Context, id int64, req *models.VisibilityRequest) (models.SellerProductRequest, error)
}

// SellerChange is the business-logic contract for the event subscriber that makes inventory match the requests.
type SellerChange interface {
	// Process brings inventory in line with the current state of each request: APPROVED and ENABLED upserts the
	// offer, APPROVED and HIDDEN withdraws it, and an ARCHIVED or REJECTED request withdraws only the variants the
	// seller no longer has live. It is idempotent; an error means "deliver again".
	Process(ctx *gofr.Context, requestIDs []int64) error
	// Reconcile applies up to limit requests whose live state has not reached inventory yet, and returns how many it
	// applied. It is the safety net under the event: a lost or failed event is repaired by the next call.
	Reconcile(ctx *gofr.Context, limit int) (int, error)
}

// Brand is the business-logic contract for brands.
type Brand interface {
	Create(ctx *gofr.Context, req *models.BrandRequest) (models.Brand, error)
	Get(ctx *gofr.Context, id int64) (models.Brand, error)
	List(ctx *gofr.Context, f *filters.BrandFilter) (models.BrandList, error)
	Update(ctx *gofr.Context, id int64, req *models.BrandRequest) (models.Brand, error)
	Delete(ctx *gofr.Context, id int64) error
}

// Category is the business-logic contract for categories.
type Category interface {
	Create(ctx *gofr.Context, req *models.CategoryRequest) (models.Category, error)
	Get(ctx *gofr.Context, id int64) (models.Category, error)
	List(ctx *gofr.Context, f *filters.CategoryFilter) (models.CategoryList, error)
	Update(ctx *gofr.Context, id int64, req *models.CategoryRequest) (models.Category, error)
	Delete(ctx *gofr.Context, id int64) error
}

// Product is the business-logic contract for products.
type Product interface {
	Create(ctx *gofr.Context, req *models.ProductRequest) (models.Product, error)
	Get(ctx *gofr.Context, id int64) (models.Product, error)
	List(ctx *gofr.Context, f *filters.ProductFilter) (models.ProductList, error)
	Update(ctx *gofr.Context, id int64, req *models.ProductRequest) (models.Product, error)
	Delete(ctx *gofr.Context, id int64) error
}

// Variant is the business-logic contract for variants.
type Variant interface {
	Create(ctx *gofr.Context, req *models.VariantRequest) (models.Variant, error)
	Get(ctx *gofr.Context, id int64) (models.Variant, error)
	List(ctx *gofr.Context, f *filters.VariantFilter) (models.VariantList, error)
	Update(ctx *gofr.Context, id int64, req *models.VariantRequest) (models.Variant, error)
	Delete(ctx *gofr.Context, id int64) error
}
