package routing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in, service, rest string
		ok                bool
	}{
		{"/api/catalogue-svc/product/42", "catalogue-svc", "/product/42", true},
		{"/api/catalogue-svc/product", "catalogue-svc", "/product", true},
		{"/api/catalogue-svc", "catalogue-svc", "/", true},
		{"/api/catalogue-svc/", "catalogue-svc", "/", true},
		{"/api/account-svc/auth/login", "account-svc", "/auth/login", true},
		{"/api/", "", "", false},
		{"/api", "", "", false},
		{"/other/x", "", "", false},
		{"/api//product", "", "", false},
	}

	for _, tc := range tests {
		service, rest, ok := Parse(tc.in)
		assert.Equal(t, tc.ok, ok, tc.in)
		assert.Equal(t, tc.service, service, tc.in)
		assert.Equal(t, tc.rest, rest, tc.in)
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern, path string
		ok            bool
		params        map[string]string
	}{
		{"/product", "/product", true, map[string]string{}},
		{"/product", "/products", false, nil},
		{"/product/{id}", "/product/42", true, map[string]string{"id": "42"}},
		{"/product/{id}", "/product", false, nil},
		{"/product/{id}", "/product/42/x", false, nil},
		{"/stock/{id}/seller/{sellerId}", "/stock/1/seller/7", true, map[string]string{"id": "1", "sellerId": "7"}},
		{"/stock/{id}/seller/{sellerId}/**", "/stock/1/seller/7/price", true, map[string]string{"id": "1", "sellerId": "7"}},
		{"/stock/{id}/seller/{sellerId}/**", "/stock/1/seller/7/price-log/x", true, map[string]string{"id": "1", "sellerId": "7"}},
		{"/stock/{id}/seller/{sellerId}/**", "/stock/1/seller/7", false, nil},
		{"/stock/{id}/seller/{sellerId}/**", "/stock/1/other/7/price", false, nil},
		{"/stock/availability", "/stock/availability", true, map[string]string{}},
		{"/stock/availability", "/stock/{id}", false, nil},
		{"/", "/", true, map[string]string{}},
	}

	for _, tc := range tests {
		params, ok := Match(tc.pattern, tc.path)
		assert.Equal(t, tc.ok, ok, "%s vs %s", tc.pattern, tc.path)
		assert.Equal(t, tc.params, params, "%s vs %s", tc.pattern, tc.path)
	}
}
