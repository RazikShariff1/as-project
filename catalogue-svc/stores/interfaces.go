// Package stores declares the data-access contracts; implementations live in one sub-package per store.
// Reads never return soft-deleted rows (deleted_at set); Delete is always a soft delete.
package stores

import (
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
)

// Seller is the data-access contract for sellers.
type Seller interface {
	Create(ctx *gofr.Context, req *models.SellerRequest) (models.Seller, error)
	Get(ctx *gofr.Context, id int64) (models.Seller, error)
	List(ctx *gofr.Context, f *filters.SellerFilter) ([]models.Seller, error)
	Update(ctx *gofr.Context, id int64, req *models.SellerRequest) (models.Seller, error)
}

// PermissibleSeller is the data-access contract for the per-product whitelist of sellers.
type PermissibleSeller interface {
	// Replace makes the listed sellers ENABLED for the product and every other listed seller DISABLED, in one
	// transaction. An unknown seller yields apperr.InvalidRef (permissible_product_sellers_seller_id_fkey).
	Replace(ctx *gofr.Context, productID int64, sellerIDs []int64) error
	// Allowed reports whether the seller is ENABLED for the product.
	Allowed(ctx *gofr.Context, sellerID, productID int64) (bool, error)
	List(ctx *gofr.Context, f *filters.PermissibleSellerFilter) (items []models.PermissibleSeller, count int, err error)
}

// SellerRequest is the data-access contract for seller product requests and their items. Reads never return
// soft-deleted requests. Every state change runs in one transaction and checks the current status inside it.
type SellerRequest interface {
	Create(ctx *gofr.Context, in *models.SellerProductRequestInput) (models.SellerProductRequest, error)
	Get(ctx *gofr.Context, id int64) (models.SellerProductRequest, error)
	List(ctx *gofr.Context, f *filters.SellerProductRequestFilter) (requests []models.SellerProductRequest, count int, err error)
	// Update replaces the status (DRAFT or PENDING) and items of a DRAFT or REJECTED request; any other status
	// yields *apperr.StateConflict.
	Update(ctx *gofr.Context, id int64, in *models.SellerProductRequestInput) (models.SellerProductRequest, error)
	// Delete soft-deletes a request that is not APPROVED (*apperr.StateConflict otherwise).
	Delete(ctx *gofr.Context, id int64) error
	// Approve makes a PENDING, REJECTED or already APPROVED request the seller's APPROVED one for the product,
	// archiving any other approved request of that seller and product. It returns the ids whose live state changed:
	// the archived ones, then this one. Approving an APPROVED request changes nothing and returns just its id, so
	// it can be repeated to re-send a lost event.
	Approve(ctx *gofr.Context, id int64) (changed []int64, err error)
	// Reject moves a PENDING request to REJECTED with a reason (*apperr.StateConflict from any other status).
	Reject(ctx *gofr.Context, id int64, reason string) (models.SellerProductRequest, error)
	// SetVisibility shows or hides an APPROVED request's live offer (*apperr.StateConflict otherwise).
	SetVisibility(ctx *gofr.Context, id int64, productStatus string) (models.SellerProductRequest, error)
	// MarkSynced records that the request's live state was applied to inventory, but only if it has not changed since
	// updatedAt, so a change made in the meantime stays unsynced for the next pass.
	MarkSynced(ctx *gofr.Context, id int64, updatedAt time.Time) error
	// Unsynced lists APPROVED and ARCHIVED requests whose live state has not been applied since they last changed.
	Unsynced(ctx *gofr.Context, limit int) ([]int64, error)
	// ApprovedVariants lists the variants the seller currently has live for the product.
	ApprovedVariants(ctx *gofr.Context, sellerID, productID int64) ([]int64, error)
}

// Brand is the data-access contract for brands.
type Brand interface {
	Create(ctx *gofr.Context, req *models.BrandRequest) (models.Brand, error)
	Get(ctx *gofr.Context, id int64) (models.Brand, error)
	List(ctx *gofr.Context, f *filters.BrandFilter) (brands []models.Brand, count int, err error)
	Update(ctx *gofr.Context, id int64, req *models.BrandRequest) (models.Brand, error)
	Delete(ctx *gofr.Context, id int64) error
}

// Category is the data-access contract for categories.
type Category interface {
	Create(ctx *gofr.Context, req *models.CategoryRequest) (models.Category, error)
	Get(ctx *gofr.Context, id int64) (models.Category, error)
	List(ctx *gofr.Context, f *filters.CategoryFilter) (categories []models.Category, count int, err error)
	Update(ctx *gofr.Context, id int64, req *models.CategoryRequest) (models.Category, error)
	Delete(ctx *gofr.Context, id int64) error
}

// Product is the data-access contract for products. Create and Update also write the product's extra
// categories (category_product) in the same transaction; reads fill CategoryIDs. Variants are loaded
// separately by the service.
type Product interface {
	Create(ctx *gofr.Context, req *models.ProductRequest) (models.Product, error)
	Get(ctx *gofr.Context, id int64) (models.Product, error)
	List(ctx *gofr.Context, f *filters.ProductFilter) (products []models.Product, count int, err error)
	Update(ctx *gofr.Context, id int64, req *models.ProductRequest) (models.Product, error)
	Delete(ctx *gofr.Context, id int64) error
}

// Variant is the data-access contract for variants.
type Variant interface {
	Create(ctx *gofr.Context, req *models.VariantRequest) (models.Variant, error)
	Get(ctx *gofr.Context, id int64) (models.Variant, error)
	List(ctx *gofr.Context, f *filters.VariantFilter) (variants []models.Variant, count int, err error)
	ListByProductIDs(ctx *gofr.Context, productIDs []int64) ([]models.Variant, error)
	Update(ctx *gofr.Context, id int64, req *models.VariantRequest) (models.Variant, error)
	Delete(ctx *gofr.Context, id int64) error
}
