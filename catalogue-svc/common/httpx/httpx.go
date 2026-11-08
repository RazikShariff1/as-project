// Package httpx holds helpers shared by the HTTP handlers and services: GoFr error mapping and pagination.
package httpx

import (
	"errors"
	"strconv"

	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/catalogue-svc/common/apperr"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

// InvalidParam builds a 400 error naming the offending parameters.
func InvalidParam(params ...string) error {
	return gofrhttp.ErrorInvalidParam{Params: params}
}

// FromStore maps a store-layer error to a GoFr HTTP error. Unknown errors surface as a 500.
func FromStore(err error, name, value string) error {
	switch {
	case errors.Is(err, apperr.ErrNotFound):
		return gofrhttp.ErrorEntityNotFound{Name: name, Value: value}
	case errors.Is(err, apperr.ErrConflict):
		return gofrhttp.ErrorEntityAlreadyExist{}
	case errors.Is(err, apperr.ErrInvalid):
		var ref *apperr.InvalidRef
		if errors.As(err, &ref) {
			if param := fkParam(ref.Constraint); param != "" {
				return InvalidParam(param)
			}
		}

		return InvalidParam(name)
	default:
		return err
	}
}

// fkParam maps a foreign-key constraint name to the request field that caused it.
func fkParam(constraint string) string {
	switch constraint {
	case "products_primary_category_id_fkey":
		return "primaryCategoryId"
	case "category_product_category_id_fkey":
		return "categoryIds"
	case "products_brand_id_fkey":
		return "brandId"
	case "categories_parent_category_id_fkey":
		return "parentCategoryId"
	case "variants_product_id_fkey", "seller_product_requests_product_id_fkey":
		return "productId"
	case "seller_product_requests_seller_id_fkey":
		return "sellerId"
	case "permissible_product_sellers_seller_id_fkey":
		return "sellerIds"
	case "permissible_product_sellers_product_id_fkey":
		return "id"
	case "seller_product_items_variant_id_fkey", "seller_product_items_mrp_nonneg", "seller_product_items_discount_within_mrp":
		return "items"
	default:
		return ""
	}
}

// Paginate reads limit (default 20, max 100), page (1-based) and offset (overrides page) from the query string.
func Paginate(ctx *gofr.Context) (limit, offset int, err error) {
	limit = defaultLimit

	if raw := ctx.Param("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > maxLimit {
			return 0, 0, InvalidParam("limit")
		}
	}

	page := 1

	if raw := ctx.Param("page"); raw != "" {
		page, err = strconv.Atoi(raw)
		if err != nil || page < 1 {
			return 0, 0, InvalidParam("page")
		}
	}

	offset = limit * (page - 1)

	if raw := ctx.Param("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return 0, 0, InvalidParam("offset")
		}
	}

	return limit, offset, nil
}

// Respond adapts a service result for a GoFr handler. GoFr turns "data plus error" into a 206/500, so on
// error the data must be nil for the error's own status code (404, 409, 400) to reach the client.
func Respond[T any](v T, err error) (any, error) {
	if err != nil {
		return nil, err
	}

	return v, nil
}

// FromStoreID is FromStore for a numeric id.
func FromStoreID(err error, name string, id int64) error {
	return FromStore(err, name, strconv.FormatInt(id, 10))
}

// PathID reads the {id} path parameter as a positive integer; anything else is a 400.
func PathID(ctx *gofr.Context) (int64, error) {
	id, err := strconv.ParseInt(ctx.PathParam("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, InvalidParam("id")
	}

	return id, nil
}

// QueryID reads an optional positive-integer query parameter; absent means 0, malformed is a 400.
func QueryID(ctx *gofr.Context, name string) (int64, error) {
	raw := ctx.Param(name)
	if raw == "" {
		return 0, nil
	}

	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, InvalidParam(name)
	}

	return id, nil
}
