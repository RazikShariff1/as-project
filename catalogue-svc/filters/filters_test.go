package filters

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSellerFilter_Clause(t *testing.T) {
	q, args := (&SellerFilter{Limit: 20, Offset: 40}).Clause()
	assert.Equal(t, " ORDER BY created_at, id LIMIT $1 OFFSET $2", q)
	assert.Equal(t, []any{20, 40}, args)

	q, args = (&SellerFilter{Status: "active", Limit: 5}).Clause()
	assert.Equal(t, " WHERE status = $1 ORDER BY created_at, id LIMIT $2 OFFSET $3", q)
	assert.Equal(t, []any{"active", 5, 0}, args)
}

func TestBrandFilter(t *testing.T) {
	where, args := (&BrandFilter{}).Where()
	assert.Equal(t, " WHERE deleted_at IS NULL", where, "soft-deleted rows are always excluded")
	assert.Empty(t, args)

	f := &BrandFilter{Name: "ac", Slug: "acme", ClientID: "c1", Status: "ENABLED", Limit: 10}
	where, args = f.Where()
	assert.Equal(t, " WHERE deleted_at IS NULL AND name ILIKE $1 AND slug = $2 AND client_id = $3 AND status = $4", where)
	assert.Equal(t, []any{"ac%", "acme", "c1", "ENABLED"}, args)

	paging, args := f.Page(args)
	assert.Equal(t, " ORDER BY name, id LIMIT $5 OFFSET $6", paging)
	assert.Equal(t, []any{"ac%", "acme", "c1", "ENABLED", 10, 0}, args)
}

func TestCategoryFilter(t *testing.T) {
	f := &CategoryFilter{ParentCategoryID: 7, Slug: "tea", ClientID: "c", Status: "HIDDEN", Limit: 10}

	where, args := f.Where()
	assert.Equal(t, " WHERE deleted_at IS NULL AND parent_category_id = $1 AND slug = $2 AND client_id = $3 AND status = $4", where)
	assert.Equal(t, []any{int64(7), "tea", "c", "HIDDEN"}, args)

	paging, _ := f.Page(args)
	assert.Equal(t, " ORDER BY name, id LIMIT $5 OFFSET $6", paging)
}

func TestProductFilter(t *testing.T) {
	where, args := (&ProductFilter{}).Where()
	assert.Equal(t, " WHERE deleted_at IS NULL", where)
	assert.Empty(t, args)

	f := &ProductFilter{
		CategoryID: 4, BrandID: 5, Status: "ENABLED", ClientItemID: "X1", Barcode: "890", Query: "tea",
		Limit: 20, Offset: 20,
	}

	where, args = f.Where()
	assert.Equal(t, " WHERE deleted_at IS NULL"+
		" AND (primary_category_id = $1 OR id IN (SELECT product_id FROM category_product WHERE category_id = $1))"+
		" AND brand_id = $2 AND status = $3 AND client_item_id = $4 AND $5 = ANY(barcodes)"+
		" AND search @@ plainto_tsquery('simple', $6)", where)
	assert.Equal(t, []any{int64(4), int64(5), "ENABLED", "X1", "890", "tea"}, args)

	paging, args := f.Page(args)
	assert.Equal(t, " ORDER BY created_at DESC, id LIMIT $7 OFFSET $8", paging)
	assert.Len(t, args, 8)
}

func TestVariantFilter(t *testing.T) {
	f := &VariantFilter{ProductID: 9, ClientItemID: "SKU", Barcode: "890", Status: "ENABLED", Limit: 5}

	where, args := f.Where()
	assert.Equal(t,
		" WHERE deleted_at IS NULL AND product_id = $1 AND client_item_id = $2 AND $3 = ANY(barcodes) AND status = $4", where)
	assert.Equal(t, []any{int64(9), "SKU", "890", "ENABLED"}, args)

	paging, args := f.Page(args)
	assert.Equal(t, " ORDER BY created_at, id LIMIT $5 OFFSET $6", paging)
	assert.Equal(t, []any{int64(9), "SKU", "890", "ENABLED", 5, 0}, args)
}

func TestPermissibleSellerFilter(t *testing.T) {
	where, args := (&PermissibleSellerFilter{}).Where()
	assert.Empty(t, where, "no soft delete here: DISABLED is the off state")
	assert.Empty(t, args)

	f := &PermissibleSellerFilter{SellerID: 7, ProductID: 3, Status: "ENABLED", Limit: 20}

	where, args = f.Where()
	assert.Equal(t, " WHERE seller_id = $1 AND product_id = $2 AND status = $3", where)

	paging, args := f.Page(args)
	assert.Equal(t, " ORDER BY product_id, seller_id LIMIT $4 OFFSET $5", paging)
	assert.Equal(t, []any{int64(7), int64(3), "ENABLED", 20, 0}, args)
}

func TestSellerProductRequestFilter(t *testing.T) {
	where, args := (&SellerProductRequestFilter{}).Where()
	assert.Equal(t, " WHERE deleted_at IS NULL", where, "soft-deleted requests are always excluded")
	assert.Empty(t, args)

	f := &SellerProductRequestFilter{SellerID: 7, ProductID: 3, Status: "PENDING", Limit: 10, Offset: 10}

	where, args = f.Where()
	assert.Equal(t, " WHERE deleted_at IS NULL AND seller_id = $1 AND product_id = $2 AND status = $3", where)

	paging, args := f.Page(args)
	assert.Equal(t, " ORDER BY id DESC LIMIT $4 OFFSET $5", paging)
	assert.Equal(t, []any{int64(7), int64(3), "PENDING", 10, 10}, args)
}
