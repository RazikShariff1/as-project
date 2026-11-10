package placeorder

import (
	"errors"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/clients/inventory"
	"github.com/zopping/order-svc/common/apperr"
	"github.com/zopping/order-svc/common/testutil"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/stores"
)

type fixture struct {
	svc       *service
	store     *stores.MockOrder
	inventory *inventory.MockClient
}

func setup(t *testing.T) fixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	f := fixture{store: stores.NewMockOrder(ctrl), inventory: inventory.NewMockClient(ctrl)}
	f.svc = New(f.store, f.inventory, 3600).(*service)

	return f
}

func f64(v float64) *float64 { return &v }

func offer(variant, seller int64, mrp, discount float64, unit int) models.Offer {
	return models.Offer{VariantID: variant, SellerID: seller, Unit: unit, Status: "ENABLED", MRP: f64(mrp),
		Discount: discount, SellingPrice: f64(mrp - discount), Tax: "5", HSNCode: "1001"}
}

func request() *models.OrderPlaceCreateSvcReq {
	return &models.OrderPlaceCreateSvcReq{
		CustomerID: 5,
		Items:      []models.OrderItemRequest{{VariantID: 10, SellerID: 7, Quantity: 2}},
		Address:    models.Address{Name: "Asha", Phone: "9999999999", Line1: "1 Main St", City: "Pune", Pincode: "411001"},
	}
}

func echo(_ *gofr.Context, o *models.Order) (*models.Order, error) { return o, nil }

func TestCreatePricesReservesBaseUnitsAndStores(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Customer)

	// 2 selling units of a variant sold by the 500 g pack: stock is held in base units, price per selling unit
	f.inventory.EXPECT().Offer(ctx, int64(10), int64(7)).Return(offer(10, 7, 120, 20, 500), nil)

	var ref string

	f.inventory.EXPECT().Reserve(ctx, gomock.Any(), []models.ReservedItem{{VariantID: 10, SellerID: 7, Quantity: 1000}}, 3600).
		DoAndReturn(func(_ *gofr.Context, r string, _ []models.ReservedItem, _ int) error { ref = r; return nil })
	f.store.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ *gofr.Context, o *models.Order) (*models.Order, error) {
		assert.Equal(t, ref, o.OrderRef, "the order is stored under the reference stock was reserved for")
		assert.Equal(t, int64(5), o.CustomerID)
		assert.Equal(t, "COD", o.PaymentMethod)
		assert.Equal(t, 240.0, o.Subtotal)
		assert.Equal(t, 40.0, o.DiscountTotal)
		assert.Equal(t, 200.0, o.Total)
		assert.Equal(t, 100.0, o.Items[0].UnitPrice)
		assert.Equal(t, 200.0, o.Items[0].LineTotal)
		assert.Equal(t, 500, o.Items[0].Unit)
		assert.Equal(t, "PENDING", o.Items[0].Status)
		assert.Len(t, o.OrderLogs, 1, "the order is stored with its first history row")
		assert.Equal(t, models.LogStatus, o.OrderLogs[0].Action)
		assert.JSONEq(t, `{"from":"","to":"PENDING"}`, string(o.OrderLogs[0].Details))

		return o, nil
	})

	got, err := f.svc.Create(ctx, request())

	assert.NoError(t, err)
	assert.Contains(t, got.OrderRef, "ORD-")
}

func TestCreateNeedsACustomer(t *testing.T) {
	f := setup(t)
	req := request()
	req.CustomerID = 0

	_, err := f.svc.Create(testutil.CtxAs(t, testutil.Staff), req)
	assert.Error(t, err, "staff must name the customer")
}

func TestCreateRejectsBadRequests(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Customer)

	noItems := request()
	noItems.Items = nil

	badQty := request()
	badQty.Items[0].Quantity = 0

	noAddress := request()
	noAddress.Address.Pincode = " "

	card := request()
	card.PaymentMethod = "CARD"

	longID := request()
	longID.ClientID = strings.Repeat("x", 65)

	bigMeta := request()
	bigMeta.MetaData = map[string]any{"blob": strings.Repeat("x", 9000)}

	for name, req := range map[string]*models.OrderPlaceCreateSvcReq{
		"no items": noItems, "zero quantity": badQty, "no pincode": noAddress, "card": card, "long clientId": longID, "big metaData": bigMeta,
	} {
		_, err := f.svc.Create(ctx, req)
		assert.Error(t, err, name)
	}
}

func TestCreateRejectsAnOfferThatCannotBeBought(t *testing.T) {
	hidden := offer(10, 7, 100, 0, 1)
	hidden.Status = "HIDDEN"

	unpriced := offer(10, 7, 100, 0, 1)
	unpriced.SellingPrice = nil

	for name, o := range map[string]models.Offer{"hidden": hidden, "unpriced": unpriced} {
		f := setup(t)
		ctx := testutil.CtxAs(t, testutil.Customer)

		f.inventory.EXPECT().Offer(ctx, gomock.Any(), gomock.Any()).Return(o, nil)

		_, err := f.svc.Create(ctx, request())
		assert.Error(t, err, name)
	}

	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Customer)
	f.inventory.EXPECT().Offer(ctx, gomock.Any(), gomock.Any()).Return(models.Offer{}, apperr.ErrNotFound)

	_, err := f.svc.Create(ctx, request())
	assert.Error(t, err, "a seller that does not sell the variant")
}

func TestCreateFailsWhenStockCannotBeReserved(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Customer)

	f.inventory.EXPECT().Offer(ctx, gomock.Any(), gomock.Any()).Return(offer(10, 7, 100, 0, 1), nil)
	f.inventory.EXPECT().Reserve(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(&apperr.StateConflict{Reason: "insufficient stock"})

	_, err := f.svc.Create(ctx, request())

	assert.EqualError(t, err, "insufficient stock", "the 409 and inventory-svc's reason reach the customer; nothing is stored or released")
}

func TestCreateReleasesTheStockWhenTheOrderCannotBeStored(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Customer)

	f.inventory.EXPECT().Offer(ctx, gomock.Any(), gomock.Any()).Return(offer(10, 7, 100, 0, 1), nil)
	f.inventory.EXPECT().Reserve(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	f.store.EXPECT().Create(ctx, gomock.Any()).Return(nil, errors.New("db down"))
	f.inventory.EXPECT().Release(ctx, gomock.Any()).Return(nil)

	_, err := f.svc.Create(ctx, request())
	assert.Error(t, err)
}

func TestAnOfferListedTwiceIsPricedOnceAndItsQuantitiesAreAdded(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Customer)
	req := request()
	req.Items = append(req.Items, models.OrderItemRequest{VariantID: 10, SellerID: 7, Quantity: 3})

	f.inventory.EXPECT().Offer(ctx, int64(10), int64(7)).Return(offer(10, 7, 100, 0, 1), nil).Times(1)
	f.inventory.EXPECT().Reserve(ctx, gomock.Any(), []models.ReservedItem{{VariantID: 10, SellerID: 7, Quantity: 5}}, gomock.Any()).Return(nil)
	f.store.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(echo)

	_, err := f.svc.Create(ctx, req)
	assert.NoError(t, err)
}

func TestCreateReadsEveryOfferAndReservesThemTogether(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Customer)
	req := request()
	req.Items = []models.OrderItemRequest{{VariantID: 10, SellerID: 7, Quantity: 1}, {VariantID: 11, SellerID: 7, Quantity: 2},
		{VariantID: 12, SellerID: 8, Quantity: 3}}

	f.inventory.EXPECT().Offer(ctx, int64(10), int64(7)).Return(offer(10, 7, 100, 0, 1), nil)
	f.inventory.EXPECT().Offer(ctx, int64(11), int64(7)).Return(offer(11, 7, 50, 0, 1), nil)
	f.inventory.EXPECT().Offer(ctx, int64(12), int64(8)).Return(offer(12, 8, 10, 0, 1), nil)
	f.inventory.EXPECT().Reserve(ctx, gomock.Any(), []models.ReservedItem{
		{VariantID: 10, SellerID: 7, Quantity: 1}, {VariantID: 11, SellerID: 7, Quantity: 2}, {VariantID: 12, SellerID: 8, Quantity: 3},
	}, gomock.Any()).Return(nil)
	f.store.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ *gofr.Context, o *models.Order) (*models.Order, error) {
		assert.Equal(t, 230.0, o.Total)

		return o, nil
	})

	_, err := f.svc.Create(ctx, req)
	assert.NoError(t, err)
}

func TestCreateReportsTheFirstUnavailableItemInRequestOrder(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Customer)
	req := request()
	req.Items = []models.OrderItemRequest{{VariantID: 10, SellerID: 7, Quantity: 1}, {VariantID: 11, SellerID: 7, Quantity: 1}}

	f.inventory.EXPECT().Offer(ctx, int64(10), int64(7)).Return(models.Offer{}, apperr.ErrNotFound)
	f.inventory.EXPECT().Offer(ctx, int64(11), int64(7)).Return(models.Offer{}, errors.New("inventory-svc down"))

	_, err := f.svc.Create(ctx, req)
	assert.ErrorContains(t, err, "variant 10", "the same error no matter which call returns first")
}

func TestPlacingAgainWithTheSameClientIDReturnsTheExistingOrder(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Customer)
	req := request()
	req.ClientID = " web-77 "

	f.store.EXPECT().GetByClientID(ctx, int64(5), "web-77").Return(&models.Order{ReferenceNumber: 41, ClientID: "web-77"}, nil)

	got, err := f.svc.Create(ctx, req)

	assert.NoError(t, err, "no offers are read, no stock is reserved, no second order is stored")
	assert.Equal(t, int64(41), got.ReferenceNumber)
}

func TestTwoRacingRequestsWithTheSameClientIDEndWithOneOrderAndOneReservation(t *testing.T) {
	f := setup(t)
	ctx := testutil.CtxAs(t, testutil.Customer)
	req := request()
	req.ClientID = "web-77"

	gomock.InOrder(
		f.store.EXPECT().GetByClientID(ctx, int64(5), "web-77").Return(nil, apperr.ErrNotFound),
		f.store.EXPECT().GetByClientID(ctx, int64(5), "web-77").Return(&models.Order{ReferenceNumber: 41}, nil),
	)
	f.inventory.EXPECT().Offer(ctx, gomock.Any(), gomock.Any()).Return(offer(10, 7, 100, 0, 1), nil)
	f.inventory.EXPECT().Reserve(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	f.store.EXPECT().Create(ctx, gomock.Any()).Return(nil, apperr.ErrDuplicateClient)
	f.inventory.EXPECT().Release(ctx, gomock.Any()).Return(nil).Times(1) // not twice: once for the duplicate, none from the defer

	got, err := f.svc.Create(ctx, req)

	assert.NoError(t, err, "the loser gives its stock back and answers with the winner's order")
	assert.Equal(t, int64(41), got.ReferenceNumber)
}
