package httpx

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/inventory-svc/common/apperr"
)

func TestFromStore(t *testing.T) {
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "7"}, FromStoreID(apperr.ErrNotFound, "id", 7))
	assert.Equal(t, gofrhttp.ErrorEntityAlreadyExist{}, FromStore(apperr.ErrConflict, "x", ""))
	assert.Equal(t, InvalidParam("fallback"), FromStore(apperr.ErrInvalid, "fallback", ""))

	other := fmt.Errorf("db down")
	assert.Equal(t, other, FromStore(other, "x", ""))
}

func TestFromStore_ConflictsAre409WithTheirOwnMessage(t *testing.T) {
	stock := FromStore(&apperr.InsufficientStock{VariantID: 3, SellerID: 8, Requested: 9, Available: 4}, "x", "")
	assert.EqualError(t, stock, "insufficient stock for variant 3 from seller 8: requested 9, available 4")
	assert.Equal(t, 409, stock.(interface{ StatusCode() int }).StatusCode()) //nolint:errorlint // asserting the concrete type

	state := FromStore(&apperr.StateConflict{Reason: "order O1 is confirmed and cannot be released"}, "x", "")
	assert.EqualError(t, state, "order O1 is confirmed and cannot be released")
	assert.Equal(t, 409, state.(interface{ StatusCode() int }).StatusCode()) //nolint:errorlint // asserting the concrete type

	wrapped := fmt.Errorf("wrapped: %w", &apperr.InsufficientStock{VariantID: 1})
	assert.Equal(t, 409, FromStore(wrapped, "x", "").(interface{ StatusCode() int }).StatusCode()) //nolint:errorlint // concrete type
}

func TestFromStore_NamesTheFailingConstraint(t *testing.T) {
	tests := map[string]string{
		"stock_reserved_within_quantity": "quantity",
		"stock_quantity_nonneg":          "quantity",
		"stock_unit_positive":            "unit",
		"stock_mrp_nonneg":               "mrp",
		"stock_discount_nonneg":          "discount",
		"stock_discount_within_mrp":      "discount",
		"stock_cost_price_nonneg":        "costPrice",
		"stock_status_valid":             "status",
		"stock_movements_reason_valid":   "reason",
	}

	for constraint, want := range tests {
		err := fmt.Errorf("wrapped: %w", &apperr.InvalidRef{Constraint: constraint})
		assert.Equal(t, InvalidParam(want), FromStore(err, "fallback", ""), constraint)
	}

	assert.Equal(t, InvalidParam("fallback"), FromStore(&apperr.InvalidRef{Constraint: "unknown"}, "fallback", ""))
}

func TestRespond_DropsDataOnError(t *testing.T) {
	data, err := Respond(struct{ A int }{}, apperr.ErrNotFound)
	assert.Nil(t, data, "GoFr turns data+error into a 500, so data must be nil")
	assert.ErrorIs(t, err, apperr.ErrNotFound)

	data, err = Respond("ok", nil)
	assert.NoError(t, err)
	assert.Equal(t, "ok", data)
}
