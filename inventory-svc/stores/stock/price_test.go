package stock

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/zopping/inventory-svc/models"
)

func str(p *string) string {
	if p == nil {
		return "<nil>"
	}

	return *p
}

func f64(f float64) *float64 { return &f }

func TestDiffNullable(t *testing.T) {
	null := sql.NullFloat64{}
	set := func(f float64) sql.NullFloat64 { return sql.NullFloat64{Float64: f, Valid: true} }

	assert.Empty(t, diffNullable("MRP", null, nil).field, "null to null is no change")
	assert.Empty(t, diffNullable("MRP", set(249.99), f64(249.99)).field, "same value is no change")
	assert.Empty(t, diffNullable("MRP", set(249.99), f64(249.990000000001)).field, "float noise is no change")

	first := diffNullable("MRP", null, f64(249))
	assert.Equal(t, "MRP", first.field)
	assert.Equal(t, "<nil>", str(first.old), "a first price has no old value")
	assert.Equal(t, "249", str(first.new))

	changed := diffNullable("MRP", set(249), f64(199.5))
	assert.Equal(t, "249", str(changed.old))
	assert.Equal(t, "199.5", str(changed.new))

	cleared := diffNullable("COST_PRICE", set(10), nil)
	assert.Equal(t, "10", str(cleared.old))
	assert.Equal(t, "<nil>", str(cleared.new), "removing a price is logged")
}

func TestDiffNumberAndText(t *testing.T) {
	assert.Empty(t, diffNumber(models.PriceFieldDiscount, 0, 0).field)

	d := diffNumber(models.PriceFieldDiscount, 0, 10)
	assert.Equal(t, "0", str(d.old))
	assert.Equal(t, "10", str(d.new))

	assert.Empty(t, diffText(models.PriceFieldTax, "GST 5%", "GST 5%").field)

	set := diffText(models.PriceFieldTax, "", "GST 5%")
	assert.Equal(t, "<nil>", str(set.old), "an empty string is stored as no value")
	assert.Equal(t, "GST 5%", str(set.new))

	cleared := diffText(models.PriceFieldHSNCode, "0902", "")
	assert.Equal(t, "0902", str(cleared.old))
	assert.Equal(t, "<nil>", str(cleared.new))
}
