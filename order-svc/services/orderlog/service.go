// Package orderlog is an order's history: reading it (a customer only sees their own orders') and adding staff entries to it.
package orderlog

import (
	"slices"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/caller"
	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/services"
	"github.com/zopping/order-svc/stores"
	"github.com/zopping/order-svc/utils"
)

// systemActions are written by order-svc itself and cannot be forged through POST /order-log.
var systemActions = []string{models.LogStatus, models.LogPayment, models.LogItem, models.LogUpdate}

type handler struct {
	store stores.OrderLog
}

// New returns the order-log service.
func New(store stores.OrderLog) services.OrderLog { return &handler{store: store} }

// Create adds a staff entry. The caller is recorded as its author.
func (h *handler) Create(ctx *gofr.Context, l *models.OrderLog) (int64, error) {
	if err := utils.StaffOnly(ctx); err != nil {
		return 0, err
	}

	l.Action = strings.ToUpper(strings.TrimSpace(l.Action))

	switch {
	case l.OrderID == 0:
		return 0, ErrOrderIDMissing
	case l.Action == "" || len(l.Action) > utils.MaxAction:
		return 0, ErrActionMissing
	case slices.Contains(systemActions, l.Action):
		return 0, httpx.InvalidParam("action")
	case len(l.Details) == 0 || string(l.Details) == "null":
		return 0, ErrDetailsMissing
	case len(l.Details) > utils.MaxMetaBytes:
		return 0, httpx.InvalidParam("details")
	}

	l.UserID = utils.UserPtr(ctx)

	created, err := h.store.Create(ctx, l)
	if err != nil {
		return 0, httpx.FromStoreID(err, "order", l.OrderID)
	}

	return created.ID, nil
}

// GetOrderID resolves a reference number to the order's id.
func (h *handler) GetOrderID(ctx *gofr.Context, referenceNumber int64) (int64, error) {
	id, err := h.store.GetOrderID(ctx, referenceNumber)
	if err != nil {
		return 0, httpx.FromStoreID(err, "order", referenceNumber)
	}

	return id, nil
}

// scoped narrows the filter to the caller's own orders when the caller is a customer.
func scoped(ctx *gofr.Context, f *filters.OrderLog) error {
	if err := validateFilter(f); err != nil {
		return err
	}

	if id, restricted := caller.Customer(ctx); restricted {
		f.CustomerID = id
	}

	return nil
}

// validateFilter requires the order whose history is asked for.
func validateFilter(f *filters.OrderLog) error {
	if f.OrderReferenceNumber == "" && f.OrderID == "" {
		return httpx.MissingParam("orderReferenceNumber")
	}

	return nil
}

// Count returns how many entries match.
func (h *handler) Count(ctx *gofr.Context, f *filters.OrderLog) (int, error) {
	if err := scoped(ctx, f); err != nil {
		return 0, err
	}

	count, err := h.store.Count(ctx, f)
	if err != nil {
		return 0, httpx.FromStore(err, "order", f.OrderReferenceNumber)
	}

	return count, nil
}

// Find returns the entries that match, newest first.
func (h *handler) Find(ctx *gofr.Context, f *filters.OrderLog) ([]*models.OrderLog, error) {
	if err := scoped(ctx, f); err != nil {
		return nil, err
	}

	logs, err := h.store.Find(ctx, f)
	if err != nil {
		return nil, httpx.FromStore(err, "order", f.OrderReferenceNumber)
	}

	return logs, nil
}
