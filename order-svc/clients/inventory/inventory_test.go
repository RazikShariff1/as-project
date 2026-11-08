package inventory

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/zopping/order-svc/common/apperr"
)

func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body))}
}

func TestCheck(t *testing.T) {
	assert.NoError(t, check(response(200, "")))
	assert.NoError(t, check(response(201, "")))
	assert.ErrorIs(t, check(response(404, "")), apperr.ErrNotFound)

	var conflict *apperr.StateConflict

	err := check(response(409, `{"error":{"message":"insufficient stock for variant 1"}}`))
	assert.ErrorAs(t, err, &conflict)
	assert.Equal(t, "insufficient stock for variant 1", conflict.Reason, "inventory-svc's own message reaches the caller")

	err = check(response(500, "boom"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestPaths(t *testing.T) {
	assert.Equal(t, "stock/10/seller/7", offerPath(10, 7))
	assert.Equal(t, "reservation/ORD-1/confirm", reservationPath("ORD-1", "confirm"))
}
