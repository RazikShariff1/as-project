package order

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"
	"gofr.dev/pkg/gofr/logging"

	"github.com/zopping/order-svc/clients/inventory"
	"github.com/zopping/order-svc/common/apperr"
	"github.com/zopping/order-svc/common/caller"
	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/stores"
)

type fixture struct {
	svc       *Service
	store     *stores.MockOrder
	inventory *inventory.MockClient
}

func setup(t *testing.T) fixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	f := fixture{store: stores.NewMockOrder(ctrl), inventory: inventory.NewMockClient(ctrl)}
	f.svc = New(f.store, f.inventory, 3600)

	return f
}

// ctxAs builds the context a service gets for a request carrying the given gateway identity headers.
func ctxAs(t *testing.T, headers map[string]string) *gofr.Context {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, "/order", nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}

	var ctx *gofr.Context

	caller.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		ctx = &gofr.Context{
			Request:       gofrhttp.NewRequest(req),
			ContextLogger: logging.ContextLoggerFor(context.Background(), logging.NewMockLogger(logging.DEBUG)),
		}
	})).ServeHTTP(httptest.NewRecorder(), r)

	return ctx
}

var (
	customer = map[string]string{"X-User-Id": "5"}
	staff    = map[string]string{"X-User-Id": "1", "X-All-Access": "true"}
)

func f64(v float64) *float64 { return &v }

func offer(variant, seller int64, mrp, discount float64, unit int) models.Offer {
	return models.Offer{VariantID: variant, SellerID: seller, Unit: unit, Status: "ENABLED", MRP: f64(mrp),
		Discount: discount, SellingPrice: f64(mrp - discount), Tax: "5", HSNCode: "1001"}
}

func request() *models.OrderRequest {
	return &models.OrderRequest{
		Items:           []models.OrderItemRequest{{VariantID: 10, SellerID: 7, Quantity: 2}},
		DeliveryAddress: models.Address{Name: "Asha", Phone: "9999999999", Line1: "1 Main St", City: "Pune", Pincode: "411001"},
	}
}

func TestPlacePricesReservesBaseUnitsAndStores(t *testing.T) {
	f := setup(t)
	ctx := ctxAs(t, customer)

	// 2 selling units of a variant sold by the 500 g pack: stock is held in base units, price per selling unit
	f.inventory.EXPECT().Offer(ctx, int64(10), int64(7)).Return(offer(10, 7, 120, 20, 500), nil)

	var ref string

	f.inventory.EXPECT().Reserve(ctx, gomock.Any(), []models.ReservedItem{{VariantID: 10, SellerID: 7, Quantity: 1000}}, 3600).
		DoAndReturn(func(_ *gofr.Context, r string, _ []models.ReservedItem, _ int) error { ref = r; return nil })
	f.store.EXPECT().Create(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, o *models.Order, _ *int64) (models.Order, error) {
			assert.Equal(t, ref, o.OrderRef, "the order is stored under the reference stock was reserved for")
			assert.Equal(t, int64(5), o.CustomerID)
			assert.Equal(t, "COD", o.PaymentMethod)
			assert.Equal(t, 240.0, o.Subtotal)
			assert.Equal(t, 40.0, o.DiscountTotal)
			assert.Equal(t, 200.0, o.Total)
			assert.Equal(t, 100.0, o.Items[0].UnitPrice)
			assert.Equal(t, 200.0, o.Items[0].LineTotal)

			return *o, nil
		})

	got, err := f.svc.Place(ctx, request())

	assert.NoError(t, err)
	assert.Contains(t, got.OrderRef, "ORD-")
}

func TestACustomerAlwaysOrdersForThemselves(t *testing.T) {
	f := setup(t)
	ctx := ctxAs(t, customer)
	req := request()
	req.CustomerID = 99

	f.inventory.EXPECT().Offer(ctx, gomock.Any(), gomock.Any()).Return(offer(10, 7, 100, 0, 1), nil)
	f.inventory.EXPECT().Reserve(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	f.store.EXPECT().Create(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, o *models.Order, _ *int64) (models.Order, error) {
			assert.Equal(t, int64(5), o.CustomerID, "the customerId in the body is ignored for a customer")

			return *o, nil
		})

	_, err := f.svc.Place(ctx, req)
	assert.NoError(t, err)
}

func TestStaffMustNameTheCustomer(t *testing.T) {
	f := setup(t)

	_, err := f.svc.Place(ctxAs(t, staff), request())
	assert.Error(t, err, "customerId is required when the caller is not a customer")
}

func TestPlaceRejectsBadRequests(t *testing.T) {
	f := setup(t)
	ctx := ctxAs(t, customer)

	noItems := request()
	noItems.Items = nil

	badQty := request()
	badQty.Items[0].Quantity = 0

	noAddress := request()
	noAddress.DeliveryAddress.Pincode = " "

	card := request()
	card.PaymentMethod = "CARD"

	for name, req := range map[string]*models.OrderRequest{"no items": noItems, "zero quantity": badQty, "no pincode": noAddress, "card": card} {
		_, err := f.svc.Place(ctx, req)
		assert.Error(t, err, name)
	}
}

func TestPlaceRejectsAnOfferThatCannotBeBought(t *testing.T) {
	hidden := offer(10, 7, 100, 0, 1)
	hidden.Status = "HIDDEN"

	unpriced := offer(10, 7, 100, 0, 1)
	unpriced.SellingPrice = nil

	for name, o := range map[string]models.Offer{"hidden": hidden, "unpriced": unpriced} {
		f := setup(t)
		ctx := ctxAs(t, customer)

		f.inventory.EXPECT().Offer(ctx, gomock.Any(), gomock.Any()).Return(o, nil)

		_, err := f.svc.Place(ctx, request())
		assert.Error(t, err, name)
	}

	f := setup(t)
	ctx := ctxAs(t, customer)
	f.inventory.EXPECT().Offer(ctx, gomock.Any(), gomock.Any()).Return(models.Offer{}, apperr.ErrNotFound)

	_, err := f.svc.Place(ctx, request())
	assert.Error(t, err, "a seller that does not sell the variant")
}

func TestPlaceFailsWhenStockCannotBeReserved(t *testing.T) {
	f := setup(t)
	ctx := ctxAs(t, customer)

	f.inventory.EXPECT().Offer(ctx, gomock.Any(), gomock.Any()).Return(offer(10, 7, 100, 0, 1), nil)
	f.inventory.EXPECT().Reserve(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(&apperr.StateConflict{Reason: "insufficient stock"})

	_, err := f.svc.Place(ctx, request())

	assert.EqualError(t, err, "insufficient stock", "the 409 and inventory-svc's reason reach the customer; nothing is stored")
}

func TestPlaceReleasesTheStockWhenTheOrderCannotBeStored(t *testing.T) {
	f := setup(t)
	ctx := ctxAs(t, customer)

	f.inventory.EXPECT().Offer(ctx, gomock.Any(), gomock.Any()).Return(offer(10, 7, 100, 0, 1), nil)
	f.inventory.EXPECT().Reserve(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	f.store.EXPECT().Create(ctx, gomock.Any(), gomock.Any()).Return(models.Order{}, errors.New("db down"))
	f.inventory.EXPECT().Release(ctx, gomock.Any()).Return(nil)

	_, err := f.svc.Place(ctx, request())
	assert.Error(t, err)
}

func TestAnOrderIsPricedOncePerOfferEvenIfListedTwice(t *testing.T) {
	f := setup(t)
	ctx := ctxAs(t, customer)
	req := request()
	req.Items = append(req.Items, models.OrderItemRequest{VariantID: 10, SellerID: 7, Quantity: 3})

	f.inventory.EXPECT().Offer(ctx, int64(10), int64(7)).Return(offer(10, 7, 100, 0, 1), nil).Times(1)
	f.inventory.EXPECT().Reserve(ctx, gomock.Any(), []models.ReservedItem{{VariantID: 10, SellerID: 7, Quantity: 5}}, gomock.Any()).Return(nil)
	f.store.EXPECT().Create(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, o *models.Order, _ *int64) (models.Order, error) { return *o, nil })

	_, err := f.svc.Place(ctx, req)
	assert.NoError(t, err)
}

func TestACustomerCannotSeeAnotherCustomersOrder(t *testing.T) {
	f := setup(t)

	f.store.EXPECT().Get(gomock.Any(), int64(3)).Return(models.Order{ID: 3, CustomerID: 6}, nil).Times(2)

	_, err := f.svc.Get(ctxAs(t, customer), 3)
	assert.Error(t, err, "someone else's order looks like it does not exist")

	got, err := f.svc.Get(ctxAs(t, staff), 3)
	assert.NoError(t, err)
	assert.Equal(t, int64(3), got.ID)
}

func TestListForcesTheCustomersOwnOrders(t *testing.T) {
	f := setup(t)
	filter := &filters.OrderFilter{CustomerID: 99, Limit: 20}

	f.store.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, fl *filters.OrderFilter) ([]models.Order, int, error) {
			assert.Equal(t, int64(5), fl.CustomerID, "a customer cannot list someone else's orders by passing customerId")

			return nil, 0, nil
		})

	_, err := f.svc.List(ctxAs(t, customer), filter)
	assert.NoError(t, err)

	f.store.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, fl *filters.OrderFilter) ([]models.Order, int, error) {
			assert.Equal(t, int64(99), fl.CustomerID, "staff may filter by any customer")

			return nil, 0, nil
		})

	_, err = f.svc.List(ctxAs(t, staff), &filters.OrderFilter{CustomerID: 99})
	assert.NoError(t, err)
}

func TestOnlyStaffMayMoveAnOrderOrRecordPayment(t *testing.T) {
	f := setup(t)

	_, err := f.svc.UpdateStatus(ctxAs(t, customer), 3, &models.StatusRequest{Status: "CONFIRMED"})
	assert.Error(t, err)

	_, err = f.svc.SetPaymentStatus(ctxAs(t, customer), 3, &models.PaymentStatusRequest{PaymentStatus: "PAID"})
	assert.Error(t, err)
}

func TestUpdateStatusRejectsStatusesStaffCannotSet(t *testing.T) {
	f := setup(t)

	for _, s := range []string{"", "PLACED", "CANCELLED", "SHIPPED"} {
		_, err := f.svc.UpdateStatus(ctxAs(t, staff), 3, &models.StatusRequest{Status: s})
		assert.Error(t, err, "%q", s)
	}
}

func TestConfirmingAnOrderDoesNotTouchStock(t *testing.T) {
	f := setup(t)
	ctx := ctxAs(t, staff)

	f.store.EXPECT().Get(ctx, int64(3)).Return(models.Order{ID: 3, Status: models.StatusPlaced}, nil)
	f.store.EXPECT().Transition(ctx, int64(3), []string{models.StatusPlaced}, models.StatusConfirmed, "ok", gomock.Any()).
		Return(models.Order{ID: 3, Status: models.StatusConfirmed}, nil)

	got, err := f.svc.UpdateStatus(ctx, 3, &models.StatusRequest{Status: "confirmed", Note: "ok"})

	assert.NoError(t, err)
	assert.Equal(t, models.StatusConfirmed, got.Status)
}

func TestPackingConfirmsTheReservedStock(t *testing.T) {
	f := setup(t)
	ctx := ctxAs(t, staff)

	f.store.EXPECT().Get(ctx, int64(3)).Return(models.Order{ID: 3, Status: models.StatusConfirmed}, nil)
	f.store.EXPECT().Transition(ctx, int64(3), []string{models.StatusConfirmed}, models.StatusPacked, "", gomock.Any()).
		Return(models.Order{ID: 3, OrderRef: "ORD-1", Status: models.StatusPacked}, nil)
	f.inventory.EXPECT().Confirm(ctx, "ORD-1").Return(nil)

	_, err := f.svc.UpdateStatus(ctx, 3, &models.StatusRequest{Status: "PACKED"})
	assert.NoError(t, err)
}

func TestRepeatingPackedRetriesTheStockConfirmation(t *testing.T) {
	f := setup(t)
	ctx := ctxAs(t, staff)
	packed := models.Order{ID: 3, OrderRef: "ORD-1", Status: models.StatusPacked}

	// first call: CONFIRMED -> PACKED is saved, but inventory-svc is down for the stock confirmation
	gomock.InOrder(
		f.store.EXPECT().Get(ctx, int64(3)).Return(models.Order{ID: 3, OrderRef: "ORD-1", Status: models.StatusConfirmed}, nil),
		f.store.EXPECT().Get(ctx, int64(3)).Return(packed, nil),
	)
	f.store.EXPECT().Transition(ctx, int64(3), []string{models.StatusConfirmed}, models.StatusPacked, gomock.Any(), gomock.Any()).
		Return(packed, nil).Times(1)
	f.inventory.EXPECT().Confirm(ctx, "ORD-1").Return(errors.New("inventory-svc down"))

	_, err := f.svc.UpdateStatus(ctx, 3, &models.StatusRequest{Status: "PACKED"})
	assert.ErrorContains(t, err, "repeat the call", "the status is saved but the stock is not confirmed yet")

	// second call: the order is already PACKED, so only the confirmation is retried
	f.inventory.EXPECT().Confirm(ctx, "ORD-1").Return(nil)

	_, err = f.svc.UpdateStatus(ctx, 3, &models.StatusRequest{Status: "PACKED"})
	assert.NoError(t, err)
}

func TestCancelMovesTheOrderAndReleasesStock(t *testing.T) {
	f := setup(t)
	ctx := ctxAs(t, customer)

	f.store.EXPECT().Get(ctx, int64(3)).Return(models.Order{ID: 3, CustomerID: 5, Status: models.StatusPlaced}, nil)
	f.store.EXPECT().Transition(ctx, int64(3), []string{models.StatusPlaced, models.StatusConfirmed}, models.StatusCancelled, "changed my mind", gomock.Any()).
		Return(models.Order{ID: 3, OrderRef: "ORD-1", Status: models.StatusCancelled}, nil)
	f.inventory.EXPECT().Release(ctx, "ORD-1").Return(nil)

	got, err := f.svc.Cancel(ctx, 3, &models.CancelRequest{Reason: " changed my mind "})

	assert.NoError(t, err)
	assert.Equal(t, models.StatusCancelled, got.Status)
}

func TestCancelOfAPackedOrderIsRefused(t *testing.T) {
	f := setup(t)
	ctx := ctxAs(t, customer)

	f.store.EXPECT().Get(ctx, int64(3)).Return(models.Order{ID: 3, CustomerID: 5, Status: models.StatusPacked}, nil)
	f.store.EXPECT().Transition(ctx, int64(3), gomock.Any(), models.StatusCancelled, gomock.Any(), gomock.Any()).
		Return(models.Order{}, &apperr.StateConflict{Reason: "the order is PACKED; it cannot move to CANCELLED"})

	_, err := f.svc.Cancel(ctx, 3, &models.CancelRequest{})
	assert.ErrorContains(t, err, "PACKED")
}

func TestRepeatingCancelRetriesTheRelease(t *testing.T) {
	f := setup(t)
	ctx := ctxAs(t, customer)

	f.store.EXPECT().Get(ctx, int64(3)).Return(models.Order{ID: 3, OrderRef: "ORD-1", CustomerID: 5, Status: models.StatusCancelled}, nil)
	f.inventory.EXPECT().Release(ctx, "ORD-1").Return(nil)

	_, err := f.svc.Cancel(ctx, 3, &models.CancelRequest{})
	assert.NoError(t, err, "no second transition; only the release is retried")
}

func TestCancelReportsAFailedRelease(t *testing.T) {
	f := setup(t)
	ctx := ctxAs(t, customer)

	f.store.EXPECT().Get(ctx, int64(3)).Return(models.Order{ID: 3, CustomerID: 5, Status: models.StatusPlaced}, nil)
	f.store.EXPECT().Transition(ctx, int64(3), gomock.Any(), models.StatusCancelled, gomock.Any(), gomock.Any()).
		Return(models.Order{ID: 3, OrderRef: "ORD-1", Status: models.StatusCancelled}, nil)
	f.inventory.EXPECT().Release(ctx, "ORD-1").Return(errors.New("inventory-svc down"))

	_, err := f.svc.Cancel(ctx, 3, &models.CancelRequest{})
	assert.ErrorContains(t, err, "repeat the call")
}

func TestACustomerCannotCancelSomeoneElsesOrder(t *testing.T) {
	f := setup(t)
	ctx := ctxAs(t, customer)

	f.store.EXPECT().Get(ctx, int64(3)).Return(models.Order{ID: 3, CustomerID: 6, Status: models.StatusPlaced}, nil)

	_, err := f.svc.Cancel(ctx, 3, &models.CancelRequest{})
	assert.Error(t, err)
}

func TestSetPaymentStatusValidatesTheStatus(t *testing.T) {
	f := setup(t)
	ctx := ctxAs(t, staff)

	_, err := f.svc.SetPaymentStatus(ctx, 3, &models.PaymentStatusRequest{PaymentStatus: "MAYBE"})
	assert.Error(t, err)

	f.store.EXPECT().SetPaymentStatus(ctx, int64(3), "PAID", "razorpay", gomock.Any()).Return(models.Order{ID: 3, PaymentStatus: "PAID"}, nil)

	got, err := f.svc.SetPaymentStatus(ctx, 3, &models.PaymentStatusRequest{PaymentStatus: "paid", Note: "razorpay"})
	assert.NoError(t, err)
	assert.Equal(t, "PAID", got.PaymentStatus)
}
