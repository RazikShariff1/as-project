package apperr

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	gofrhttp "gofr.dev/pkg/gofr/http"
)

// These errors are returned straight to the handler, so they must carry their own status or GoFr answers 500.
func TestWorkflowErrorsCarryTheirOwnHTTPStatus(t *testing.T) {
	tests := []struct {
		err  error
		code int
	}{
		{&Forbidden{Reason: "the seller is not permitted to sell this product"}, http.StatusForbidden},
		{&StateConflict{Reason: "a approved request cannot be edited"}, http.StatusConflict},
	}

	for _, tc := range tests {
		var responder gofrhttp.StatusCodeResponder

		assert.ErrorAs(t, tc.err, &responder, "%T must implement GoFr's StatusCodeResponder", tc.err)
		assert.Equal(t, tc.code, responder.StatusCode(), "%T", tc.err)
	}
}

func TestInvalidRefIsErrInvalid(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &InvalidRef{Constraint: "products_brand_id_fkey"})

	assert.True(t, errors.Is(err, ErrInvalid))
	assert.False(t, errors.Is(err, ErrConflict))
	assert.Contains(t, err.Error(), "products_brand_id_fkey")
}
