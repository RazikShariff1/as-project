package httpx

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/catalogue-svc/common/apperr"
)

func TestFromStore(t *testing.T) {
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "7"}, FromStoreID(apperr.ErrNotFound, "id", 7))
	assert.Equal(t, gofrhttp.ErrorEntityAlreadyExist{}, FromStore(apperr.ErrConflict, "x", ""))
	assert.Equal(t, InvalidParam("fallback"), FromStore(apperr.ErrInvalid, "fallback", ""))

	other := fmt.Errorf("db down")
	assert.Equal(t, other, FromStore(other, "x", ""))
}

func TestFromStore_NamesTheFailingForeignKey(t *testing.T) {
	tests := map[string]string{
		"products_primary_category_id_fkey":  "primaryCategoryId",
		"category_product_category_id_fkey":  "categoryIds",
		"products_brand_id_fkey":             "brandId",
		"categories_parent_category_id_fkey": "parentCategoryId",
		"variants_product_id_fkey":           "productId",
	}

	for constraint, want := range tests {
		err := fmt.Errorf("wrapped: %w", &apperr.InvalidRef{Constraint: constraint})
		assert.Equal(t, InvalidParam(want), FromStore(err, "fallback", ""), constraint)
	}

	assert.Equal(t, InvalidParam("fallback"), FromStore(&apperr.InvalidRef{Constraint: "unknown_fkey"}, "fallback", ""))
}

func TestRespond_DropsDataOnError(t *testing.T) {
	data, err := Respond(struct{ A int }{}, apperr.ErrNotFound)
	assert.Nil(t, data, "GoFr turns data+error into a 500, so data must be nil")
	assert.ErrorIs(t, err, apperr.ErrNotFound)

	data, err = Respond("ok", nil)
	assert.NoError(t, err)
	assert.Equal(t, "ok", data)
}
