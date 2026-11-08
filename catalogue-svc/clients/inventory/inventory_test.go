package inventory

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body))}
}

func TestCheck(t *testing.T) {
	assert.NoError(t, check(response(200, ""), "offer", false))
	assert.NoError(t, check(response(204, ""), "withdraw", true))

	err := check(response(409, `{"error":{"message":"insufficient stock"}}`), "offer for variant 1, seller 2", false)
	assert.Error(t, err, "a 4xx from inventory fails the call so the event is redelivered")
	assert.Contains(t, err.Error(), "status 409")
	assert.Contains(t, err.Error(), "insufficient stock", "the response body explains why")

	assert.Error(t, check(response(500, "boom"), "offer", false))
	assert.Error(t, check(response(404, ""), "offer", false), "a missing variant is an error when upserting")
	assert.NoError(t, check(response(404, ""), "withdraw", true), "withdrawing an offer that does not exist is already done")
	assert.Error(t, check(response(502, ""), "withdraw", true), "but a gateway error is not")
}

func TestOfferPath(t *testing.T) {
	assert.Equal(t, "stock/10/seller/7", offerPath(10, 7))
}
