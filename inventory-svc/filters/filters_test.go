package filters

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStockFilter(t *testing.T) {
	where, args := (&StockFilter{}).Where()
	assert.Empty(t, where)
	assert.Empty(t, args)

	f := &StockFilter{VariantID: 5, SellerID: 2, Status: "HIDDEN", LowStock: true, OutOfStock: true, Limit: 20, Offset: 40}

	where, args = f.Where()
	assert.Equal(t, " WHERE variant_id = $1 AND seller_id = $2 AND status = $3"+
		" AND NOT unlimited_stock AND low_stock_threshold IS NOT NULL AND quantity - reserved <= low_stock_threshold"+
		" AND NOT unlimited_stock AND quantity - reserved <= 0", where)
	assert.Equal(t, []any{int64(5), int64(2), "HIDDEN"}, args)

	paging, args := f.Page(args)
	assert.Equal(t, " ORDER BY variant_id, seller_id LIMIT $4 OFFSET $5", paging)
	assert.Equal(t, []any{int64(5), int64(2), "HIDDEN", 20, 40}, args)
}

func TestMovementFilter(t *testing.T) {
	f := &MovementFilter{VariantID: 7, SellerID: 3, Limit: 10, Offset: 10}

	where, args := f.Where()
	assert.Equal(t, " WHERE variant_id = $1 AND seller_id = $2", where)

	paging, args := f.Page(args)
	assert.Equal(t, " ORDER BY id DESC LIMIT $3 OFFSET $4", paging)
	assert.Equal(t, []any{int64(7), int64(3), 10, 10}, args)
}

func TestPriceLogFilter(t *testing.T) {
	f := &PriceLogFilter{VariantID: 3, SellerID: 9, Limit: 10, Offset: 20}

	where, args := f.Where()
	assert.Equal(t, " WHERE variant_id = $1 AND seller_id = $2", where)

	paging, args := f.Page(args)
	assert.Equal(t, " ORDER BY id DESC LIMIT $3 OFFSET $4", paging)
	assert.Equal(t, []any{int64(3), int64(9), 10, 20}, args)
}
