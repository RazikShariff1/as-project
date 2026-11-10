package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/order-svc/clients/inventory"
	"github.com/zopping/order-svc/common/apperr"
	"github.com/zopping/order-svc/common/testutil"
	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/stores"
)

type fixture struct {
	svc       OrderService
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

func ptr[T any](v T) *T { return &v }

// pendingOrder has two lines: variant 10 (2 x 500 g packs at 100) and variant 11 (1 at 50).
func pendingOrder() *models.Order {
	return &models.Order{
		ReferenceNumber: 3, OrderRef: "ORD-OLD", Status: models.StatusPending, PaymentStatus: models.PaymentPending,
		PaymentMethod: models.MethodCOD, CustomerID: 5, Subtotal: 290, DiscountTotal: 40, Total: 250,
		Items: []models.OrderItem{
			{ID: 1, VariantID: 10, SellerID: 7, Quantity: 2, Unit: 500, Status: "PENDING", MRP: 120, Discount: 20, UnitPrice: 100, LineTotal: 200},
			{ID: 2, VariantID: 11, SellerID: 7, Quantity: 1, Unit: 1, Status: "PENDING", MRP: 50, UnitPrice: 50, LineTotal: 50},
		},
	}
}

func withStatus(o *models.Order, status string) *models.Order {
	o.Status = status

	return o
}

// found makes the store answer the load of order 3 (for the caller's scope: customerID 0 is staff).
func (f fixture) found(o *models.Order, customerID int64) {
	f.store.EXPECT().Index(gomock.Any(), &filters.OrderDetails{ReferenceNumbers: []int64{3}, CustomerID: customerID}, 1, 0).
		Return([]*models.Order{o}, nil)
}

func updateEcho(_ *gofr.Context, _ int64, o *models.Order) (*models.Order, error) { return o, nil }

func TestPatchRejectsRequestsACustomerMayNotMake(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Customer)

	for name, p := range map[string]*PatchOrderPayload{
		"pack":         {CustomerID: 5, Status: "PACKED"},
		"payment":      {CustomerID: 5, PaymentStatus: "PAID"},
		"metadata":     {CustomerID: 5, MetaData: map[string]any{"a": 1}},
		"item changes": {CustomerID: 5, OrderItems: []*ItemPayload{{ID: 1, Quantity: ptr(int64(1))}}},
	} {
		_, err := f.svc.Patch(ctx, 3, p)

		var forbidden *apperr.Forbidden

		assert.ErrorAs(t, err, &forbidden, name)
	}
}

func TestPatchRejectsBadRequests(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Staff)

	for name, p := range map[string]*PatchOrderPayload{
		"empty":                     {},
		"unknown status":            {Status: "SHIPPED"},
		"in-store status":           {Status: "PICKING"},
		"back to pending":           {Status: "PENDING"},
		"unknown payment status":    {PaymentStatus: "MAYBE"},
		"reason without cancelling": {Status: "PACKED", CancellationReason: "x"},
		"incomplete address":        {DeliveryAddress: &models.Address{Name: "Asha"}},
		"long notes":                {Notes: ptr(strings.Repeat("x", 501))},
		"long note":                 {Status: "PACKED", Note: strings.Repeat("x", 501)},
		"huge metadata":             {MetaData: map[string]any{"blob": strings.Repeat("x", 9000)}},
		"item without a quantity":   {OrderItems: []*ItemPayload{{ID: 1}}},
		"item edited twice":         {OrderItems: []*ItemPayload{{ID: 1, Quantity: ptr(int64(1))}, {ID: 1, Quantity: ptr(int64(2))}}},
		"negative quantity":         {OrderItems: []*ItemPayload{{ID: 1, Quantity: ptr(int64(-1))}}},
	} {
		_, err := f.svc.Patch(ctx, 3, p)
		assert.Error(t, err, name)
	}
}

func TestPatchOfAMissingOrIsNotTheCallersOrderIsA404(t *testing.T) {
	f := setup(t)

	f.store.EXPECT().Index(gomock.Any(), &filters.OrderDetails{ReferenceNumbers: []int64{3}, CustomerID: 5}, 1, 0).Return(nil, nil)

	_, err := f.svc.Patch(testutil.CtxAs(t, testutil.Customer), 3, &PatchOrderPayload{CustomerID: 5, Status: "CANCELLED"})

	var notFound gofrhttp.ErrorEntityNotFound

	assert.ErrorAs(t, err, &notFound, "a 404, not a 403: it must not reveal that the order exists")
}

func TestPackingSavesTheStatusAndItsHistoryThenConfirmsTheReservedStock(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Staff)
	old := pendingOrder()

	f.found(old, 0)
	f.store.EXPECT().Update(ctx, int64(3), gomock.Any()).DoAndReturn(func(_ *gofr.Context, _ int64, o *models.Order) (*models.Order, error) {
		assert.Equal(t, models.StatusPacked, o.Status)
		assert.Equal(t, old.UpdatedAt, o.UpdatedAt, "the order goes back with the version it was read with")
		assert.Len(t, o.OrderLogs, 1)
		assert.JSONEq(t, `{"from":"PENDING","to":"PACKED","note":"boxed"}`, string(o.OrderLogs[0].Details))

		return o, nil
	})
	f.inventory.EXPECT().Confirm(ctx, "ORD-OLD").Return(nil)

	got, err := f.svc.Patch(ctx, 3, &PatchOrderPayload{Status: "packed", Note: "boxed"})

	assert.NoError(t, err)
	assert.Equal(t, models.StatusPacked, got.Status)
	assert.Equal(t, models.StatusPending, old.Status, "the order that was read is not touched")
}

func TestOnlyTheStepsOfTheLifecycleAreAllowed(t *testing.T) {
	for name, tc := range map[string]struct{ from, to string }{
		"skipping a step":          {models.StatusPending, "COMPLETED"},
		"dispatching before pack":  {models.StatusPending, "DISPATCHED"},
		"cancelling a packed one":  {models.StatusPacked, "CANCELLED"},
		"going backwards":          {models.StatusDispatched, "PACKED"},
		"cancelling a completed":   {models.StatusCompleted, "CANCELLED"},
		"cancelling a cancelled":   {models.StatusCancelled, "PACKED"},
		"dispatching a dispatched": {models.StatusDispatched, "DISPATCHED"}, // stays: repeating only works for PACKED/CANCELLED
	} {
		f := setup(t)
		ctx := testutil.CtxAs(t, testutil.Staff)

		if tc.from == tc.to {
			continue
		}

		f.found(withStatus(pendingOrder(), tc.from), 0)

		_, err := f.svc.Patch(ctx, 3, &PatchOrderPayload{Status: tc.to})
		assert.ErrorContains(t, err, "cannot move to", name)
	}
}

func TestDispatchingAndCompletingDoNotTouchStockAndCompletingAnCODOrderMarksItPaid(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Staff)

	f.found(withStatus(pendingOrder(), models.StatusPacked), 0)
	f.store.EXPECT().Update(ctx, int64(3), gomock.Any()).DoAndReturn(updateEcho)

	_, err := f.svc.Patch(ctx, 3, &PatchOrderPayload{Status: "DISPATCHED"})
	assert.NoError(t, err)

	f.found(withStatus(pendingOrder(), models.StatusDispatched), 0)
	f.store.EXPECT().Update(ctx, int64(3), gomock.Any()).DoAndReturn(func(_ *gofr.Context, _ int64, o *models.Order) (*models.Order, error) {
		assert.NotNil(t, o.CompletedAt, "the moment it was completed is kept, for the completedBetween filter")
		assert.Equal(t, models.PaymentPaid, o.PaymentStatus, "cash is collected on delivery")
		assert.Len(t, o.OrderLogs, 2, "a status row and a payment row, in the same save")

		return o, nil
	})

	got, err := f.svc.Patch(ctx, 3, &PatchOrderPayload{Status: "COMPLETED"})
	assert.NoError(t, err)
	assert.Equal(t, models.StatusCompleted, got.Status)
}

func TestRepeatingPackedWritesNothingAndRetriesTheStockConfirmation(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Staff)

	f.store.EXPECT().Index(gomock.Any(), gomock.Any(), 1, 0).Return([]*models.Order{withStatus(pendingOrder(), models.StatusPacked)}, nil).Times(2)
	gomock.InOrder(
		f.inventory.EXPECT().Confirm(ctx, "ORD-OLD").Return(errors.New("inventory-svc down")),
		f.inventory.EXPECT().Confirm(ctx, "ORD-OLD").Return(nil),
	)

	_, err := f.svc.Patch(ctx, 3, &PatchOrderPayload{Status: "PACKED"})
	assert.ErrorContains(t, err, "repeat the call", "the status is saved but the stock is not confirmed yet")

	_, err = f.svc.Patch(ctx, 3, &PatchOrderPayload{Status: "PACKED"})
	assert.NoError(t, err, "no store update: only the stock call is retried")
}

func TestACustomerCancelsTheirOwnOrderAndItsStockIsReleased(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Customer)

	f.found(pendingOrder(), 5)
	f.store.EXPECT().Update(ctx, int64(3), gomock.Any()).DoAndReturn(func(_ *gofr.Context, _ int64, o *models.Order) (*models.Order, error) {
		assert.Equal(t, models.StatusCancelled, o.Status)
		assert.Equal(t, "changed my mind", o.CancelReason)
		assert.JSONEq(t, `{"from":"PENDING","to":"CANCELLED","note":"changed my mind"}`, string(o.OrderLogs[0].Details))

		return o, nil
	})
	f.inventory.EXPECT().Release(ctx, "ORD-OLD").Return(nil)

	got, err := f.svc.Patch(ctx, 3, &PatchOrderPayload{CustomerID: 5, Status: "CANCELLED", CancellationReason: " changed my mind "})

	assert.NoError(t, err)
	assert.Equal(t, models.StatusCancelled, got.Status)
}

func TestRepeatingCancelRetriesTheReleaseAndReportsAFailure(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Customer)

	f.store.EXPECT().Index(gomock.Any(), gomock.Any(), 1, 0).Return([]*models.Order{withStatus(pendingOrder(), models.StatusCancelled)}, nil).Times(2)
	gomock.InOrder(
		f.inventory.EXPECT().Release(ctx, "ORD-OLD").Return(errors.New("inventory-svc down")),
		f.inventory.EXPECT().Release(ctx, "ORD-OLD").Return(nil),
	)

	_, err := f.svc.Patch(ctx, 3, &PatchOrderPayload{CustomerID: 5, Status: "CANCELLED"})
	assert.ErrorContains(t, err, "repeat the call")

	_, err = f.svc.Patch(ctx, 3, &PatchOrderPayload{CustomerID: 5, Status: "CANCELLED"})
	assert.NoError(t, err)
}

func TestACancelledOrdersReleaseToleratesAReservationInventoryNoLongerHas(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Staff)

	f.found(pendingOrder(), 0)
	f.store.EXPECT().Update(ctx, int64(3), gomock.Any()).DoAndReturn(updateEcho)
	f.inventory.EXPECT().Release(ctx, "ORD-OLD").Return(apperr.ErrNotFound)

	_, err := f.svc.Patch(ctx, 3, &PatchOrderPayload{Status: "CANCELLED"})
	assert.NoError(t, err)
}

func TestPaymentAndDetailChangesAreSavedTogetherAndNeverTouchStock(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Staff)
	old := pendingOrder()
	old.MetaData = map[string]any{"source": "web", "keep": true}

	f.found(old, 0)
	f.store.EXPECT().Update(ctx, int64(3), gomock.Any()).DoAndReturn(func(_ *gofr.Context, _ int64, o *models.Order) (*models.Order, error) {
		assert.Equal(t, models.PaymentPaid, o.PaymentStatus)
		assert.Equal(t, "leave at the gate", o.Notes)
		assert.Equal(t, "Pune", o.DeliveryAddress.City)
		assert.Equal(t, map[string]any{"source": "app", "keep": true}, o.MetaData, "metadata is merged, not replaced")
		assert.Equal(t, models.StatusPending, o.Status)
		assert.Len(t, o.OrderLogs, 4, "payment, address, notes and metadata each leave a row")

		return o, nil
	})

	_, err := f.svc.Patch(ctx, 3, &PatchOrderPayload{
		PaymentStatus: "paid", Notes: ptr(" leave at the gate "), MetaData: map[string]any{"source": "app"},
		DeliveryAddress: &models.Address{Name: "Asha", Phone: "9", Line1: "1 Main St", City: " Pune ", Pincode: "411001"},
	})
	assert.NoError(t, err)
}

func TestAddressAndNotesCannotChangeAfterDispatch(t *testing.T) {
	f := setup(t)

	f.found(withStatus(pendingOrder(), models.StatusDispatched), 0)

	_, err := f.svc.Patch(testutil.CtxAs(t, testutil.Staff), 3, &PatchOrderPayload{Notes: ptr("x")})
	assert.ErrorContains(t, err, "before it is dispatched")
}

func TestChangesThatChangeNothingAreNotSaved(t *testing.T) {
	f := setup(t)

	f.found(pendingOrder(), 0)

	_, err := f.svc.Patch(testutil.CtxAs(t, testutil.Staff), 3, &PatchOrderPayload{PaymentStatus: "PENDING", Notes: ptr("")})
	assert.NoError(t, err, "no store update")
}

func TestASaveOverANewerVersionIsAConflict(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Staff)

	f.found(pendingOrder(), 0)
	f.store.EXPECT().Update(ctx, int64(3), gomock.Any()).
		Return(nil, &apperr.StateConflict{Reason: "the order was changed by another request; read it and try again"})

	_, err := f.svc.Patch(ctx, 3, &PatchOrderPayload{Status: "PACKED"})

	assert.ErrorContains(t, err, "changed by another request")
}

func TestEditingItemsReservesTheNewQuantitiesSwapsThemInAndReleasesTheOldReservation(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Staff)

	var newRef string

	f.found(pendingOrder(), 0)
	f.inventory.EXPECT().Reserve(ctx, gomock.Any(), []models.ReservedItem{{VariantID: 10, SellerID: 7, Quantity: 500}}, 3600).
		DoAndReturn(func(_ *gofr.Context, r string, _ []models.ReservedItem, _ int) error { newRef = r; return nil })
	f.store.EXPECT().Update(ctx, int64(3), gomock.Any()).DoAndReturn(func(_ *gofr.Context, _ int64, o *models.Order) (*models.Order, error) {
		assert.Equal(t, newRef, o.OrderRef, "the order is switched to the new reservation")
		assert.Equal(t, []string{"ORD-OLD"}, o.StaleRefs, "the old one is released after the save")
		assert.Equal(t, 100.0, o.Total, "the removed line no longer counts")
		assert.Equal(t, 120.0, o.Subtotal)
		assert.Equal(t, 20.0, o.DiscountTotal)
		assert.Equal(t, "CANCELLED", o.Items[1].Status)
		assert.Equal(t, int64(1), o.Items[1].Quantity, "a removed line keeps its quantity for the record")
		assert.Equal(t, int64(1), o.Items[0].Quantity)
		assert.Len(t, o.OrderLogs, 2)

		return o, nil
	})
	f.inventory.EXPECT().Release(ctx, "ORD-OLD").Return(nil)
	f.store.EXPECT().ClearStaleRef(ctx, int64(3), "ORD-OLD").Return(nil)

	got, err := f.svc.Patch(ctx, 3, &PatchOrderPayload{OrderItems: []*ItemPayload{
		{ID: 1, Quantity: ptr(int64(1)), Reason: "customer asked"}, {ID: 2, Quantity: ptr(int64(0)), Reason: "out of stock"},
	}})

	assert.NoError(t, err)
	assert.Equal(t, newRef, got.OrderRef)
}

func TestEditingItemsReleasesTheNewReservationWhenTheOrderCannotBeSaved(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Staff)

	f.found(pendingOrder(), 0)
	f.inventory.EXPECT().Reserve(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	f.store.EXPECT().Update(ctx, int64(3), gomock.Any()).
		Return(nil, &apperr.StateConflict{Reason: "the order was changed by another request; read it and try again"})
	f.inventory.EXPECT().Release(ctx, gomock.Any()).DoAndReturn(func(_ *gofr.Context, r string) error {
		assert.NotEqual(t, "ORD-OLD", r, "only the reservation made for this attempt is given back")

		return nil
	})

	_, err := f.svc.Patch(ctx, 3, &PatchOrderPayload{OrderItems: []*ItemPayload{{ID: 1, Quantity: ptr(int64(1))}}})
	assert.ErrorContains(t, err, "changed by another request")
}

func TestEditingItemsFailsWhenTheNewQuantityIsNotInStock(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Staff)

	f.found(pendingOrder(), 0)
	f.inventory.EXPECT().Reserve(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(&apperr.StateConflict{Reason: "insufficient stock"})

	_, err := f.svc.Patch(ctx, 3, &PatchOrderPayload{OrderItems: []*ItemPayload{{ID: 1, Quantity: ptr(int64(5))}}})
	assert.EqualError(t, err, "insufficient stock", "nothing is saved and the old reservation is untouched")
}

func TestEditingItemsRefusesWhatCannotWork(t *testing.T) {
	removed := pendingOrder()
	removed.Items[1].Status = models.ItemCancelled

	for name, tc := range map[string]struct {
		order *models.Order
		edits []*ItemPayload
	}{
		"removing every line":     {pendingOrder(), []*ItemPayload{{ID: 1, Quantity: ptr(int64(0))}, {ID: 2, Quantity: ptr(int64(0))}}},
		"an unknown line":         {pendingOrder(), []*ItemPayload{{ID: 99, Quantity: ptr(int64(1))}}},
		"an order already packed": {withStatus(pendingOrder(), models.StatusPacked), []*ItemPayload{{ID: 1, Quantity: ptr(int64(1))}}},
		"a line already removed":  {removed, []*ItemPayload{{ID: 2, Quantity: ptr(int64(1))}}},
	} {
		f := setup(t)

		f.found(tc.order, 0)

		_, err := f.svc.Patch(testutil.CtxAs(t, testutil.Staff), 3, &PatchOrderPayload{OrderItems: tc.edits})
		assert.Error(t, err, name)
	}
}

func TestEditingItemsToTheQuantitiesAlreadyOnTheOrderReservesAndSavesNothing(t *testing.T) {
	f := setup(t)

	f.found(pendingOrder(), 0)

	_, err := f.svc.Patch(testutil.CtxAs(t, testutil.Staff), 3, &PatchOrderPayload{OrderItems: []*ItemPayload{{ID: 1, Quantity: ptr(int64(2))}}})
	assert.NoError(t, err)
}

func TestAReplacedReservationThatCannotBeReleasedDoesNotFailTheUpdate(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Staff)

	f.found(pendingOrder(), 0)
	f.store.EXPECT().Update(ctx, int64(3), gomock.Any()).DoAndReturn(func(_ *gofr.Context, _ int64, o *models.Order) (*models.Order, error) {
		o.StaleRefs = []string{"ORD-STALE"}

		return o, nil
	})
	f.inventory.EXPECT().Release(ctx, "ORD-STALE").Return(errors.New("inventory-svc down"))

	_, err := f.svc.Patch(ctx, 3, &PatchOrderPayload{PaymentStatus: "PAID"})

	assert.NoError(t, err, "the order is saved; the stale hold is retried by the next update and expires on its own")
}
