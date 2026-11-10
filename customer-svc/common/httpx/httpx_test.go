package httpx

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/customer-svc/common/apperr"
)

func status(err error) int {
	coded, ok := err.(interface{ StatusCode() int }) //nolint:errorlint // asserting the concrete type

	if !ok {
		return 0
	}

	return coded.StatusCode()
}

func TestFromStore(t *testing.T) {
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "7"}, FromStoreID(apperr.ErrNotFound, "id", 7))
	assert.Equal(t, gofrhttp.ErrorEntityAlreadyExist{}, FromStore(apperr.ErrConflict, "x", ""))
	assert.Equal(t, InvalidParam("fallback"), FromStore(apperr.ErrInvalid, "fallback", ""))

	other := fmt.Errorf("db down")
	assert.Equal(t, other, FromStore(other, "x", ""))
	assert.NoError(t, FromStore(nil, "x", ""), "nil stays nil, so callers can wrap unconditionally")
}

func TestFromStore_AuthErrorsKeepTheirOwnStatusAndMessage(t *testing.T) {
	tests := []struct {
		err  error
		code int
	}{
		{&apperr.Unauthorized{Reason: "invalid credentials"}, http.StatusUnauthorized},
		{&apperr.Forbidden{Reason: "account is suspended"}, http.StatusForbidden},
		{&apperr.Locked{Reason: "too many failed login attempts"}, http.StatusTooManyRequests},
		{fmt.Errorf("wrapped: %w", &apperr.Forbidden{Reason: "nope"}), http.StatusForbidden},
	}

	for _, tc := range tests {
		got := FromStore(tc.err, "x", "")
		assert.Equal(t, tc.code, status(got), tc.err.Error())
	}

	assert.EqualError(t, FromStore(&apperr.Unauthorized{Reason: "invalid credentials"}, "x", ""), "invalid credentials")
}

func TestFromStore_NamesTheFailingConstraint(t *testing.T) {
	tests := map[string]string{
		"customers_status_valid": "status",
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
