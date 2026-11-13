package order

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/order-svc/common/testutil"
	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/stores"
)

func setup(t *testing.T) (*stores.MockOrder, *service) {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	store := stores.NewMockOrder(ctrl)

	return store, New(store, nil, nil).(*service)
}

func TestACustomerCannotReadAnotherCustomersOrder(t *testing.T) {
	store, svc := setup(t)

	store.EXPECT().Retrieve(gomock.Any(), int64(3)).Return(&models.Order{ReferenceNumber: 3, CustomerID: 6}, nil).Times(2)

	_, err := svc.Read(testutil.CtxAs(t, testutil.Customer), &filters.OrderDetails{ReferenceNumbers: []int64{3}})

	var notFound gofrhttp.ErrorEntityNotFound

	assert.ErrorAs(t, err, &notFound, "someone else's order looks like it does not exist")

	got, err := svc.Read(testutil.CtxAs(t, testutil.Staff), &filters.OrderDetails{ReferenceNumbers: []int64{3}})
	assert.NoError(t, err)
	assert.Equal(t, int64(3), got.ReferenceNumber)
}

func TestReadOfAMissingOrderIsA404(t *testing.T) {
	store, svc := setup(t)

	store.EXPECT().Retrieve(gomock.Any(), int64(3)).Return(nil, errors.New("not found"))

	_, err := svc.Read(testutil.CtxAs(t, testutil.Staff), &filters.OrderDetails{ReferenceNumbers: []int64{3}})
	assert.Error(t, err)
}

func TestFindForcesTheCustomersOwnOrders(t *testing.T) {
	store, svc := setup(t)

	check := func(want int64) {
		store.EXPECT().Index(gomock.Any(), gomock.Any(), 20, 0).DoAndReturn(
			func(_ *gofr.Context, f *filters.OrderDetails, _, _ int) ([]*models.Order, error) {
				assert.Equal(t, want, f.CustomerID)

				return nil, nil
			})
		store.EXPECT().Count(gomock.Any(), gomock.Any()).DoAndReturn(func(_ *gofr.Context, f *filters.OrderDetails) (int, error) {
			assert.Equal(t, want, f.CustomerID, "the count is scoped the same way as the page")

			return 0, nil
		})
	}

	check(5)

	_, err := svc.Find(testutil.CtxAs(t, testutil.Customer), &filters.OrderDetails{CustomerID: 99}, &models.Pagination{Limit: 20})
	assert.NoError(t, err, "a customer cannot list someone else's orders by passing customerId")

	check(99)

	_, err = svc.Find(testutil.CtxAs(t, testutil.Staff), &filters.OrderDetails{CustomerID: 99}, &models.Pagination{Limit: 20})
	assert.NoError(t, err, "staff may filter by any customer")
}

func TestFindReturnsThePageAndTheTotal(t *testing.T) {
	store, svc := setup(t)

	store.EXPECT().Index(gomock.Any(), gomock.Any(), 10, 20).Return([]*models.Order{{ReferenceNumber: 1}}, nil)
	store.EXPECT().Count(gomock.Any(), gomock.Any()).Return(31, nil)

	got, err := svc.Find(testutil.CtxAs(t, testutil.Staff), &filters.OrderDetails{}, &models.Pagination{Limit: 10, Offset: 20})

	assert.NoError(t, err)
	assert.Equal(t, 31, got.Count)
	assert.Equal(t, 20, got.Offset)
	assert.Len(t, got.Orders, 1)
}

func TestFindFailsIfEitherQueryFails(t *testing.T) {
	store, svc := setup(t)

	store.EXPECT().Index(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	store.EXPECT().Count(gomock.Any(), gomock.Any()).Return(0, errors.New("db down"))

	_, err := svc.Find(testutil.CtxAs(t, testutil.Staff), &filters.OrderDetails{}, &models.Pagination{Limit: 10})
	assert.Error(t, err)
}
