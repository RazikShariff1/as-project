package apperr

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	gofrhttp "gofr.dev/pkg/gofr/http"
)

// GoFr turns an error into an HTTP status only if it implements StatusCodeResponder. The auth service returns these
// types straight to the handler, so they must carry their own status or every denial becomes a 500.
func TestAuthErrorsCarryTheirOwnHTTPStatus(t *testing.T) {
	tests := []struct {
		err  error
		code int
	}{
		{&Unauthorized{Reason: "invalid credentials"}, http.StatusUnauthorized},
		{&Forbidden{Reason: "account is suspended"}, http.StatusForbidden},
		{&Locked{Reason: "too many failed login attempts"}, http.StatusTooManyRequests},
		{&Unavailable{Reason: "google sign-in is not enabled"}, http.StatusServiceUnavailable},
	}

	for _, tc := range tests {
		var responder gofrhttp.StatusCodeResponder

		assert.ErrorAs(t, tc.err, &responder, "%T must implement GoFr's StatusCodeResponder", tc.err)
		assert.Equal(t, tc.code, responder.StatusCode(), "%T", tc.err)
	}
}

func TestInvalidRefIsErrInvalid(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &InvalidRef{Constraint: "user_role_role_id_fkey"})

	assert.True(t, errors.Is(err, ErrInvalid))
	assert.False(t, errors.Is(err, ErrConflict))
	assert.Contains(t, err.Error(), "user_role_role_id_fkey")
}
