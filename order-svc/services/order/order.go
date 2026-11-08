// Package order holds the business rules for orders: pricing from inventory-svc, stock reservation, the status lifecycle and
// who may see or change which order.
package order

import (
	"errors"
	"fmt"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/clients/inventory"
	"github.com/zopping/order-svc/common/apperr"
	"github.com/zopping/order-svc/common/caller"
	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/services"
	"github.com/zopping/order-svc/stores"
)

var _ services.Order = (*Service)(nil)

// allowedFrom lists, for each status staff may set, the statuses it can be reached from. CANCELLED has its own endpoint.
func allowedFrom() map[string][]string {
	return map[string][]string{
		models.StatusConfirmed:      {models.StatusPlaced},
		models.StatusPacked:         {models.StatusConfirmed},
		models.StatusOutForDelivery: {models.StatusPacked},
		models.StatusDelivered:      {models.StatusOutForDelivery},
	}
}

// Service implements services.Order.
type Service struct {
	store     stores.Order
	inventory inventory.Client
	ttl       int
}

// New returns an order service. ttlSeconds is how long a new order holds its stock before inventory-svc may release it.
func New(store stores.Order, inv inventory.Client, ttlSeconds int) *Service {
	return &Service{store: store, inventory: inv, ttl: ttlSeconds}
}

// ValidTTL reports whether ttlSeconds is acceptable to inventory-svc.
func ValidTTL(ttlSeconds int) bool { return ttlSeconds >= 1 && ttlSeconds <= maxTTLSeconds }

func userPtr(ctx *gofr.Context) *int64 {
	if id, ok := caller.User(ctx); ok {
		return &id
	}

	return nil
}

func notFound(id int64) error { return httpx.FromStoreID(apperr.ErrNotFound, "order", id) }

func staffOnly(ctx *gofr.Context) error {
	if _, restricted := caller.Customer(ctx); restricted {
		return &apperr.Forbidden{Reason: "only staff may do this"}
	}

	return nil
}

func offerError(err error, it models.OrderItemRequest) error {
	if errors.Is(err, apperr.ErrNotFound) {
		return unavailable(it)
	}

	return err
}

// Place prices the items, reserves their stock and stores the order. If storing fails the reservation is released.
func (s *Service) Place(ctx *gofr.Context, req *models.OrderRequest) (models.Order, error) {
	customer, err := customerFor(ctx, req)
	if err != nil {
		return models.Order{}, err
	}

	if err = normalise(req); err != nil {
		return models.Order{}, err
	}

	items, reserve, err := s.priceItems(ctx, req)
	if err != nil {
		return models.Order{}, err
	}

	ref, err := newOrderRef()
	if err != nil {
		return models.Order{}, err
	}

	if err = s.inventory.Reserve(ctx, ref, reserve, s.ttl); err != nil {
		return models.Order{}, httpx.FromStore(err, "order", ref)
	}

	o := models.Order{
		OrderRef: ref, CustomerID: customer, PaymentMethod: req.PaymentMethod, DeliveryAddress: req.DeliveryAddress,
		Notes: req.Notes, Items: items,
	}
	o.Subtotal, o.DiscountTotal, o.Total = totals(items)

	created, err := s.store.Create(ctx, &o, userPtr(ctx))
	if err != nil {
		if relErr := s.inventory.Release(ctx, ref); relErr != nil {
			ctx.Errorf("order %s was not stored and its stock could not be released (it expires on its own): %v", ref, relErr)
		}

		return models.Order{}, httpx.FromStore(err, "order", ref)
	}

	return created, nil
}

// visible reports whether the caller may see o: a customer only sees their own orders.
func visible(ctx *gofr.Context, o *models.Order) bool {
	id, restricted := caller.Customer(ctx)

	return !restricted || o.CustomerID == id
}

// Get returns an order, or a 404 if it is not the caller's.
func (s *Service) Get(ctx *gofr.Context, id int64) (models.Order, error) {
	o, err := s.store.Get(ctx, id)
	if err != nil {
		return models.Order{}, httpx.FromStoreID(err, "order", id)
	}

	if !visible(ctx, &o) {
		return models.Order{}, notFound(id)
	}

	return o, nil
}

// List returns a page of orders. A customer only ever lists their own, whatever the filter says.
func (s *Service) List(ctx *gofr.Context, f *filters.OrderFilter) (models.OrderList, error) {
	if id, restricted := caller.Customer(ctx); restricted {
		f.CustomerID = id
	}

	orders, count, err := s.store.List(ctx, f)
	if err != nil {
		return models.OrderList{}, httpx.FromStore(err, "order", "")
	}

	return models.OrderList{Orders: orders, Count: count, Offset: f.Offset, Limit: f.Limit}, nil
}

// UpdateStatus moves an order forward. PACKED then confirms the reserved stock as a sale; if that fails the status is already
// saved, so repeating the call retries the confirmation.
func (s *Service) UpdateStatus(ctx *gofr.Context, id int64, req *models.StatusRequest) (models.Order, error) {
	if err := staffOnly(ctx); err != nil {
		return models.Order{}, err
	}

	req.Status = strings.ToUpper(strings.TrimSpace(req.Status))

	from, ok := allowedFrom()[req.Status]
	if !ok {
		return models.Order{}, httpx.InvalidParam("status")
	}

	current, err := s.Get(ctx, id)
	if err != nil {
		return models.Order{}, err
	}

	if current.Status == req.Status && req.Status == models.StatusPacked {
		return current, s.confirmStock(ctx, current.OrderRef)
	}

	updated, err := s.store.Transition(ctx, id, from, req.Status, strings.TrimSpace(req.Note), userPtr(ctx))
	if err != nil {
		return models.Order{}, httpx.FromStoreID(err, "order", id)
	}

	if req.Status == models.StatusPacked {
		return updated, s.confirmStock(ctx, updated.OrderRef)
	}

	return updated, nil
}

func (s *Service) confirmStock(ctx *gofr.Context, orderRef string) error {
	if err := s.inventory.Confirm(ctx, orderRef); err != nil {
		return fmt.Errorf("the order is packed but its stock could not be confirmed: repeat the call to retry: %w", err)
	}

	return nil
}

// Cancel cancels a PLACED or CONFIRMED order and releases its stock. Repeating it on a cancelled order retries the release.
func (s *Service) Cancel(ctx *gofr.Context, id int64, req *models.CancelRequest) (models.Order, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return models.Order{}, err
	}

	if current.Status == models.StatusCancelled {
		return current, s.releaseStock(ctx, current.OrderRef)
	}

	reason := strings.TrimSpace(req.Reason)
	if len(reason) > maxNotes {
		return models.Order{}, httpx.InvalidParam("reason")
	}

	updated, err := s.store.Transition(ctx, id, []string{models.StatusPlaced, models.StatusConfirmed}, models.StatusCancelled,
		reason, userPtr(ctx))
	if err != nil {
		return models.Order{}, httpx.FromStoreID(err, "order", id)
	}

	return updated, s.releaseStock(ctx, updated.OrderRef)
}

func (s *Service) releaseStock(ctx *gofr.Context, orderRef string) error {
	err := s.inventory.Release(ctx, orderRef)
	if err == nil || errors.Is(err, apperr.ErrNotFound) {
		return nil
	}

	return fmt.Errorf("the order is cancelled but its stock could not be released: repeat the call to retry: %w", err)
}

// SetPaymentStatus records the payment outcome.
func (s *Service) SetPaymentStatus(ctx *gofr.Context, id int64, req *models.PaymentStatusRequest) (models.Order, error) {
	if err := staffOnly(ctx); err != nil {
		return models.Order{}, err
	}

	req.PaymentStatus = strings.ToUpper(strings.TrimSpace(req.PaymentStatus))

	switch req.PaymentStatus {
	case models.PaymentPending, models.PaymentPaid, models.PaymentFailed, models.PaymentRefunded:
	default:
		return models.Order{}, httpx.InvalidParam("paymentStatus")
	}

	o, err := s.store.SetPaymentStatus(ctx, id, req.PaymentStatus, strings.TrimSpace(req.Note), userPtr(ctx))
	if err != nil {
		return models.Order{}, httpx.FromStoreID(err, "order", id)
	}

	return o, nil
}

// Logs returns an order's history, after checking the caller may see the order.
func (s *Service) Logs(ctx *gofr.Context, id int64) ([]models.OrderLog, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}

	logs, err := s.store.Logs(ctx, id)
	if err != nil {
		return nil, httpx.FromStoreID(err, "order", id)
	}

	return logs, nil
}
