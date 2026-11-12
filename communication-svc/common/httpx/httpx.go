// Package httpx holds helpers shared by the HTTP handlers and services: GoFr error mapping and request parsing.
package httpx

import (
	"errors"
	"net/http"
	"strconv"

	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/communication-svc/common/apperr"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

// coded is an error with its own HTTP status.
type coded struct {
	msg  string
	code int
}

func (c coded) Error() string   { return c.msg }
func (c coded) StatusCode() int { return c.code }

// InvalidParam builds a 400 error naming the offending parameters.
func InvalidParam(params ...string) error {
	return gofrhttp.ErrorInvalidParam{Params: params}
}

// FromStore maps a store or service error to a GoFr HTTP error. Unknown errors surface as a 500.
func FromStore(err error, name, value string) error {
	var (
		unauthorized *apperr.Unauthorized
		forbidden    *apperr.Forbidden
		locked       *apperr.Locked
		ref          *apperr.InvalidRef
	)

	switch {
	case errors.As(err, &unauthorized):
		return coded{msg: unauthorized.Reason, code: http.StatusUnauthorized}
	case errors.As(err, &forbidden):
		return coded{msg: forbidden.Reason, code: http.StatusForbidden}
	case errors.As(err, &locked):
		return coded{msg: locked.Reason, code: http.StatusTooManyRequests}
	case errors.Is(err, apperr.ErrNotFound):
		return gofrhttp.ErrorEntityNotFound{Name: name, Value: value}
	case errors.Is(err, apperr.ErrConflict):
		return gofrhttp.ErrorEntityAlreadyExist{}
	case errors.Is(err, apperr.ErrInvalid):
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

// FromStoreID is FromStore for a numeric id.
func FromStoreID(err error, name string, id int64) error {
	return FromStore(err, name, strconv.FormatInt(id, 10))
}

// fkParam maps a failing constraint name to the request field that caused it.
func fkParam(constraint string) string {
	switch constraint {
	case "customers_status_valid":
		return "status"
	default:
		return ""
	}
}

// Respond adapts a service result for a GoFr handler. GoFr turns "data plus error" into a 206/500, so on
// error the data must be nil for the error's own status code (404, 409, 400) to reach the client.
func Respond[T any](v T, err error) (any, error) {
	if err != nil {
		return nil, err
	}

	return v, nil
}

// PathID reads the {id} path parameter as a positive integer; anything else is a 400.
func PathID(ctx *gofr.Context) (int64, error) {
	id, err := strconv.ParseInt(ctx.PathParam("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, InvalidParam("id")
	}

	return id, nil
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

// BadRequest builds a 400 error with its own message, for input a client can fix that no single parameter explains
// (for example a template field the request left out).
func BadRequest(msg string) error { return coded{msg: msg, code: http.StatusBadRequest} }
