package order

import (
	"strconv"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
)

const maxListValues = 50

// orderFilter reads the list filters from the query string.
func orderFilter(ctx *gofr.Context) (*filters.OrderDetails, error) {
	var (
		filter = &filters.OrderDetails{}
		err    error
	)

	if filter.CustomerID, err = httpx.QueryID(ctx, "customerId"); err != nil {
		return nil, err
	}

	if filter.SellerID, err = httpx.QueryID(ctx, "sellerID"); err != nil {
		return nil, err
	}

	// id is the order's reference number, like referenceNumber but for one.
	id, err := httpx.QueryID(ctx, "id")
	if err != nil {
		return nil, err
	}

	if filter.Statuses, err = getStatuses(ctx.Param("status")); err != nil {
		return nil, err
	}

	if filter.ReferenceNumbers, err = getReferenceNumbers(ctx.Param("referenceNumber")); err != nil {
		return nil, err
	}

	if id != 0 {
		filter.ReferenceNumbers = append(filter.ReferenceNumbers, id)
	}

	switch filter.PaymentStatus = strings.ToUpper(ctx.Param("paymentStatus")); filter.PaymentStatus {
	case "", models.PaymentPending, models.PaymentPaid, models.PaymentCancelled, models.PaymentRefund:
	default:
		return nil, httpx.InvalidParam("paymentStatus")
	}

	if filter.PlacedFrom, filter.PlacedTo, err = getBetween(ctx.Param("placedBetween"), "placedBetween"); err != nil {
		return nil, err
	}

	if filter.CompletedFrom, filter.CompletedTo, err = getBetween(ctx.Param("completedBetween"), "completedBetween"); err != nil {
		return nil, err
	}

	filter.UpdatedSince, err = getUpdatedSince(ctx.Param("updatedSince"))

	return filter, err
}

// updatedSinceLayouts are the formats updatedSince may be sent in.
var updatedSinceLayouts = []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", time.DateOnly}

// getUpdatedSince reads a moment (RFC 3339, or a date or date-time without a zone, taken as UTC); empty means any.
func getUpdatedSince(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}

	for _, layout := range updatedSinceLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), nil
		}
	}

	return time.Time{}, httpx.InvalidParam("updatedSince")
}

func split(raw string) []string {
	var out []string

	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}

	return out
}

func getStatuses(raw string) ([]string, error) {
	parts := split(raw)
	if len(parts) > maxListValues {
		return nil, httpx.InvalidParam("status")
	}

	for i, p := range parts {
		parts[i] = strings.ToUpper(p)

		switch parts[i] {
		case models.StatusPending, models.StatusPacked, models.StatusDispatched, models.StatusCompleted, models.StatusCancelled:
		default:
			return nil, httpx.InvalidParam("status")
		}
	}

	return parts, nil
}

func getReferenceNumbers(raw string) ([]int64, error) {
	parts := split(raw)
	if len(parts) > maxListValues {
		return nil, httpx.InvalidParam("referenceNumber")
	}

	refs := make([]int64, 0, len(parts))

	for _, p := range parts {
		ref, err := strconv.ParseInt(p, 10, 64)
		if err != nil || ref < 1 {
			return nil, httpx.InvalidParam("referenceNumber")
		}

		refs = append(refs, ref)
	}

	return refs, nil
}

// getBetween reads "from,to" (YYYY-MM-DD, both inclusive); either side may be empty.
func getBetween(raw, name string) (from, to time.Time, err error) {
	if raw == "" {
		return from, to, nil
	}

	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return from, to, httpx.InvalidParam(name)
	}

	for i, dst := range []*time.Time{&from, &to} {
		if p := strings.TrimSpace(parts[i]); p != "" {
			if *dst, err = time.Parse(time.DateOnly, p); err != nil {
				return from, to, httpx.InvalidParam(name)
			}
		}
	}

	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		return from, to, httpx.InvalidParam(name)
	}

	return from, to, nil
}
