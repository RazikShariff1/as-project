// Package httpx holds helpers shared by the HTTP handlers and services: GoFr error mapping and pagination.
package httpx

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/inventory-svc/common/apperr"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

func isInsufficient(err error) bool {
	var e *apperr.InsufficientStock

	return errors.As(err, &e)
}

func isStateConflict(err error) bool {
	var e *apperr.StateConflict

	return errors.As(err, &e)
}

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
	case isInsufficient(err):
		return Conflict(err.Error())
	case isStateConflict(err):
		return Conflict(err.Error())
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

// fkParam maps a failing constraint name to the request field that caused it.
func fkParam(constraint string) string {
	switch constraint {
	case "stock_reserved_within_quantity", "stock_quantity_nonneg":
		return "quantity"
	case "stock_mrp_nonneg":
		return "mrp"
	case "stock_discount_nonneg", "stock_discount_within_mrp":
		return "discount"
	case "stock_cost_price_nonneg":
		return "costPrice"
	case "stock_unit_positive":
		return "unit"
	case "stock_status_valid":
		return "status"
	case "stock_movements_reason_valid":
		return "reason"
	default:
		return ""
	}
}

// conflict is a 409 with a specific message.
type conflict struct{ msg string }

func (c conflict) Error() string { return c.msg }
func (conflict) StatusCode() int { return http.StatusConflict }

// Conflict builds a 409 error with the given message.
func Conflict(msg string) error { return conflict{msg: msg} }

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

const maxIDs = 100

// QueryIDs reads a required comma-separated list of positive integers (at most 100), e.g. variantIds=1,2,3.
func QueryIDs(ctx *gofr.Context, name string) ([]int64, error) {
	raw := strings.TrimSpace(ctx.Param(name))
	if raw == "" {
		return nil, InvalidParam(name)
	}

	parts := strings.Split(raw, ",")
	if len(parts) > maxIDs {
		return nil, InvalidParam(name)
	}

	ids := make([]int64, 0, len(parts))

	for _, p := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil || id < 1 {
			return nil, InvalidParam(name)
		}

		ids = append(ids, id)
	}

	return ids, nil
}

// QueryBool reads an optional boolean query parameter; absent means false, anything unparseable is a 400.
func QueryBool(ctx *gofr.Context, name string) (bool, error) {
	raw := ctx.Param(name)
	if raw == "" {
		return false, nil
	}

	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, InvalidParam(name)
	}

	return v, nil
}

// PathSellerID reads the {sellerId} path parameter as a positive integer; anything else is a 400.
func PathSellerID(ctx *gofr.Context) (int64, error) {
	id, err := strconv.ParseInt(ctx.PathParam("sellerId"), 10, 64)
	if err != nil || id < 1 {
		return 0, InvalidParam("sellerId")
	}

	return id, nil
}
