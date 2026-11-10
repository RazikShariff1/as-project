package orderlog

import (
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/apperr"
	"github.com/zopping/order-svc/common/testutil"
	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/stores"
)

func setup(t *testing.T) (*stores.MockOrderLog, *handler) {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	store := stores.NewMockOrderLog(ctrl)

	return store, New(store).(*handler)
}

func entry(action string) *models.OrderLog {
	return &models.OrderLog{OrderID: 3, Action: action, Details: []byte(`{"note":"called the customer"}`)}
}

func TestOnlyStaffAddEntries(t *testing.T) {
	_, svc := setup(t)

	_, err := svc.Create(testutil.CtxAs(t, testutil.Customer), entry("NOTE"))

	var forbidden *apperr.Forbidden

	assert.ErrorAs(t, err, &forbidden)
}

func TestCreateRejectsWhatIsMissingOrForged(t *testing.T) {
	_, svc := setup(t)
	ctx := testutil.CtxAs(t, testutil.Staff)

	noOrder := entry("NOTE")
	noOrder.OrderID = 0

	noDetails := entry("NOTE")
	noDetails.Details = nil

	nullDetails := entry("NOTE")
	nullDetails.Details = []byte("null")

	for name, l := range map[string]*models.OrderLog{
		"no order": noOrder, "no action": entry(" "), "no details": noDetails, "null details": nullDetails,
		"forged status": entry("status"), "forged payment": entry("PAYMENT"), "long action": entry(strings.Repeat("x", 51)),
	} {
		_, err := svc.Create(ctx, l)
		assert.Error(t, err, name)
	}
}

func TestCreateStoresTheEntryAsTheCaller(t *testing.T) {
	store, svc := setup(t)
	ctx := testutil.CtxAs(t, testutil.Staff)

	store.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ *gofr.Context, l *models.OrderLog) (*models.OrderLog, error) {
		assert.Equal(t, "NOTE", l.Action, "the action is upper-cased")
		assert.Equal(t, int64(1), *l.UserID, "the author is the caller, not something the body says")

		return &models.OrderLog{ID: 9}, nil
	})

	id, err := svc.Create(ctx, entry("note"))

	assert.NoError(t, err)
	assert.Equal(t, int64(9), id)
}

func TestFindAndCountNeedTheOrder(t *testing.T) {
	_, svc := setup(t)
	ctx := testutil.CtxAs(t, testutil.Staff)

	_, err := svc.Find(ctx, &filters.OrderLog{Action: "STATUS"})
	assert.Error(t, err)

	_, err = svc.Count(ctx, &filters.OrderLog{})
	assert.Error(t, err)
}

func TestACustomerOnlySeesTheirOwnOrdersHistory(t *testing.T) {
	store, svc := setup(t)

	store.EXPECT().Find(gomock.Any(), gomock.Any()).DoAndReturn(func(_ *gofr.Context, f *filters.OrderLog) ([]*models.OrderLog, error) {
		assert.Equal(t, int64(5), f.CustomerID, "the query joins to the customer's own orders, so someone else's has no rows")

		return nil, nil
	})
	store.EXPECT().Count(gomock.Any(), gomock.Any()).DoAndReturn(func(_ *gofr.Context, f *filters.OrderLog) (int, error) {
		assert.Equal(t, int64(5), f.CustomerID)

		return 0, nil
	})

	_, err := svc.Find(testutil.CtxAs(t, testutil.Customer), &filters.OrderLog{OrderReferenceNumber: "3", CustomerID: 99})
	assert.NoError(t, err)

	_, err = svc.Count(testutil.CtxAs(t, testutil.Customer), &filters.OrderLog{OrderReferenceNumber: "3"})
	assert.NoError(t, err)

	store.EXPECT().Find(gomock.Any(), gomock.Any()).DoAndReturn(func(_ *gofr.Context, f *filters.OrderLog) ([]*models.OrderLog, error) {
		assert.Zero(t, f.CustomerID, "staff see every order's history")

		return []*models.OrderLog{{ID: 1}}, nil
	})

	got, err := svc.Find(testutil.CtxAs(t, testutil.Staff), &filters.OrderLog{OrderReferenceNumber: "3"})
	assert.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestGetOrderIDOfAMissingOrderIsA404(t *testing.T) {
	store, svc := setup(t)

	store.EXPECT().GetOrderID(gomock.Any(), int64(3)).Return(int64(0), apperr.ErrNotFound)

	_, err := svc.GetOrderID(testutil.CtxAs(t, testutil.Staff), 3)
	assert.Error(t, err)
}
