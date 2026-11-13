package order

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/clients/catalogue"
	"github.com/zopping/order-svc/clients/customer"
	"github.com/zopping/order-svc/common/testutil"
	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/stores"
)

func setupExtra(t *testing.T) (*stores.MockOrder, *customer.MockClient, *catalogue.MockClient, *service) {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	store, customers, cat := stores.NewMockOrder(ctrl), customer.NewMockClient(ctrl), catalogue.NewMockClient(ctrl)

	return store, customers, cat, New(store, customers, cat).(*service)
}

func TestFindAddsCustomersAndItemNames(t *testing.T) {
	store, customers, cat, svc := setupExtra(t)

	store.EXPECT().Index(gomock.Any(), gomock.Any(), 20, 0).Return([]*models.Order{
		{ReferenceNumber: 1, CustomerID: 6, Items: []models.OrderItem{{VariantID: 11}, {VariantID: 12}}},
		{ReferenceNumber: 2, CustomerID: 6, Items: []models.OrderItem{{VariantID: 11}}},
	}, nil)
	store.EXPECT().Count(gomock.Any(), gomock.Any()).Return(2, nil)

	customers.EXPECT().Customer(gomock.Any(), int64(6)).Return(&models.Customer{ID: 6, Name: "Asha"}, nil).Times(1)
	cat.EXPECT().Variant(gomock.Any(), int64(11)).Return(&models.CatalogueVariant{Name: "1L", ProductID: 3}, nil).Times(1)
	cat.EXPECT().Variant(gomock.Any(), int64(12)).Return(nil, errors.New("down")).Times(1)
	cat.EXPECT().Product(gomock.Any(), int64(3)).Return(&models.CatalogueProduct{Name: "Milk"}, nil).Times(1)

	list, err := svc.Find(testutil.CtxAs(t, testutil.Staff), &filters.OrderDetails{IncludeItemNames: true}, &models.Pagination{Limit: 20})

	assert.NoError(t, err)
	assert.Equal(t, "Asha", list.Orders[0].Customer.Name)
	assert.Equal(t, "Asha", list.Orders[1].Customer.Name, "one customer is read once however many orders they have")
	assert.Equal(t, "Milk - 1L", list.Orders[0].Items[0].Name)
	assert.Empty(t, list.Orders[0].Items[1].Name, "a variant that could not be read is left unnamed, the read still works")
}

func TestExtraFieldsAreOptional(t *testing.T) {
	store, _, _, svc := setupExtra(t)

	store.EXPECT().Retrieve(gomock.Any(), int64(1)).Return(&models.Order{ReferenceNumber: 1, CustomerID: 6, Items: []models.OrderItem{{VariantID: 11}}}, nil)

	// No mock calls are expected: the customer is excluded and item names were not asked for.
	got, err := svc.Read(testutil.CtxAs(t, testutil.Staff), &filters.OrderDetails{ReferenceNumbers: []int64{1}, ExcludeCustomerDetail: true})

	assert.NoError(t, err)
	assert.Nil(t, got.Customer)
	assert.Len(t, got.Items, 1)
	assert.Empty(t, got.Items[0].Name)
}

// slowCustomers answers after a pause and records how many calls were in flight together.
type slowCustomers struct{ active, peak atomic.Int32 }

func (s *slowCustomers) Customer(_ *gofr.Context, id int64) (*models.Customer, error) {
	n := s.active.Add(1)
	defer s.active.Add(-1)

	for p := s.peak.Load(); n > p && !s.peak.CompareAndSwap(p, n); p = s.peak.Load() {
	}

	time.Sleep(30 * time.Millisecond)

	return &models.Customer{ID: id}, nil
}

func TestCustomersAreReadInParallel(t *testing.T) {
	svc := &service{customers: &slowCustomers{}}

	got := svc.getCustomerIDToCustomer(testutil.CtxAs(t, testutil.Staff), []int64{1, 2, 3, 4})

	assert.Len(t, got, 4)
	assert.Greater(t, svc.customers.(*slowCustomers).peak.Load(), int32(1), "the calls overlap")
}
