// Package placeorder places orders: validate the request, price the items from inventory-svc, reserve their stock and store the
// order. The stages follow the reference service's Create.
package placeorder

import (
	"errors"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/clients/inventory"
	"github.com/zopping/order-svc/common/apperr"
	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/services"
	"github.com/zopping/order-svc/stores"
	"github.com/zopping/order-svc/utils"
)

const maxTTLSeconds = 24 * 60 * 60

type service struct {
	store     stores.Order
	inventory inventory.Client
	ttl       int
}

// New returns the place-order service. ttlSeconds is how long a new order holds its stock before inventory-svc may release it.
func New(store stores.Order, inv inventory.Client, ttlSeconds int) services.PlaceOrder {
	return &service{store: store, inventory: inv, ttl: ttlSeconds}
}

// ValidTTL reports whether ttlSeconds is acceptable to inventory-svc.
func ValidTTL(ttlSeconds int) bool { return ttlSeconds >= 1 && ttlSeconds <= maxTTLSeconds }

// Create validates the request, prices the items, reserves their stock and stores the order with its first history row. If storing
// fails the reservation is released. A clientId makes it idempotent: the customer's order with that clientId is returned instead of
// a second one, also when two identical requests race (the unique index lets one in and the other releases its stock and returns
// the winner).
func (s *service) Create(ctx *gofr.Context, req *models.OrderPlaceCreateSvcReq) (order *models.Order, err error) {
	if err = s.validateOrderCreate(req); err != nil {
		return nil, err
	}

	if existing, found, lookupErr := s.getPlacedOrder(ctx, req); lookupErr != nil || found {
		return existing, lookupErr
	}

	items, reserve, err := s.processOrderItems(ctx, req)
	if err != nil {
		return nil, err
	}

	ref, err := utils.NewOrderRef()
	if err != nil {
		return nil, err
	}

	if err = s.reserveStock(ctx, ref, reserve); err != nil {
		return nil, err
	}

	// Whatever fails from here on, the stock held for this attempt goes back.
	defer func() {
		if err != nil {
			s.unreserveStock(ctx, ref, "the order was not stored")
		}
	}()

	o := &models.Order{
		OrderRef: ref, ClientID: req.ClientID, CustomerID: req.CustomerID, PaymentMethod: req.PaymentMethod,
		DeliveryAddress: req.Address, Notes: req.Notes, MetaData: req.MetaData, Items: items,
	}
	o.Subtotal, o.DiscountTotal, o.Total = utils.Totals(items)
	o.OrderLogs = s.createOrderLogs(req)

	order, err = s.store.Create(ctx, o)
	if errors.Is(err, apperr.ErrDuplicateClient) {
		// A request with the same clientId won the race: give this one's stock back and answer with the winner's order.
		s.unreserveStock(ctx, ref, "the order was a duplicate")

		existing, found, lookupErr := s.getPlacedOrder(ctx, req)
		if lookupErr != nil || !found {
			return nil, lookupErr
		}

		return existing, nil
	}

	if err != nil {
		return nil, httpx.FromStore(err, "order", ref)
	}

	return order, nil
}

// createOrderLogs builds the history written with the order: it was placed and is PENDING.
func (*service) createOrderLogs(req *models.OrderPlaceCreateSvcReq) []*models.OrderLog {
	return []*models.OrderLog{utils.NewLog(0, req.UserID, models.LogStatus, utils.Change("", models.StatusPending, ""))}
}

// getPlacedOrder finds the customer's order with this clientId, if they sent one and it exists.
func (s *service) getPlacedOrder(ctx *gofr.Context, req *models.OrderPlaceCreateSvcReq) (*models.Order, bool, error) {
	if req.ClientID == "" {
		return nil, false, nil
	}

	o, err := s.store.GetByClientID(ctx, req.CustomerID, req.ClientID)

	switch {
	case err == nil:
		return o, true, nil
	case errors.Is(err, apperr.ErrNotFound):
		return nil, false, nil
	default:
		return nil, false, httpx.FromStore(err, "order", req.ClientID)
	}
}

// reserveStock holds the items (base units) for the order, all or nothing.
func (s *service) reserveStock(ctx *gofr.Context, ref string, reserve []models.ReservedItem) error {
	if err := s.inventory.Reserve(ctx, ref, reserve, s.ttl); err != nil {
		return httpx.FromStore(err, "order", ref)
	}

	return nil
}

// unreserveStock gives a reservation back where the caller has nothing better to do with a failure: the hold expires on its own.
func (s *service) unreserveStock(ctx *gofr.Context, ref, why string) {
	if err := s.inventory.Release(ctx, ref); err != nil && !errors.Is(err, apperr.ErrNotFound) {
		ctx.Errorf("%s and stock reservation %s could not be released (it expires on its own): %v", why, ref, err)
	}
}
