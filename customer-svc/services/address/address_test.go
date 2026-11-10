package address

import (
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/customer-svc/common/apperr"
	"github.com/zopping/customer-svc/models"
	"github.com/zopping/customer-svc/stores"
)

func setup(t *testing.T) (*Service, *stores.MockAddress, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	m := stores.NewMockAddress(ctrl)

	return New(m), m, &gofr.Context{}
}

func valid() *models.AddressRequest {
	return &models.AddressRequest{Recipient: " Asha ", Line1: "12 MG Road", City: "Bengaluru", PostalCode: "560001"}
}

func TestCreate_TrimsAndSavesForTheCallersCustomer(t *testing.T) {
	svc, m, ctx := setup(t)

	m.EXPECT().List(ctx, int64(5)).Return(nil, nil)
	m.EXPECT().Create(ctx, int64(5), &models.AddressRequest{Recipient: "Asha", Line1: "12 MG Road", City: "Bengaluru", PostalCode: "560001"}).
		Return(models.Address{ID: 1, CustomerID: 5}, nil)

	a, err := svc.Create(ctx, 5, valid())
	assert.NoError(t, err)
	assert.Equal(t, int64(5), a.CustomerID)
}

func TestCreate_RequiredFields(t *testing.T) {
	svc, _, ctx := setup(t)

	for field, mutate := range map[string]func(*models.AddressRequest){
		"recipient":  func(r *models.AddressRequest) { r.Recipient = " " },
		"line1":      func(r *models.AddressRequest) { r.Line1 = "" },
		"city":       func(r *models.AddressRequest) { r.City = "" },
		"postalCode": func(r *models.AddressRequest) { r.PostalCode = "" },
	} {
		req := valid()
		mutate(req)

		_, err := svc.Create(ctx, 5, req)
		assert.Equal(t, gofrhttp.ErrorInvalidParam{Params: []string{field}}, err, field)
	}
}

func TestCreate_StopsAtTheLimit(t *testing.T) {
	svc, m, ctx := setup(t)

	m.EXPECT().List(ctx, int64(5)).Return(make([]models.Address, maxAddresses), nil)

	_, err := svc.Create(ctx, 5, valid())
	assert.Error(t, err)
}

func TestAnotherCustomersAddressIsNotFound(t *testing.T) {
	svc, m, ctx := setup(t)

	m.EXPECT().Get(ctx, int64(5), int64(9)).Return(models.Address{}, apperr.ErrNotFound)
	m.EXPECT().Delete(ctx, int64(5), int64(9)).Return(apperr.ErrNotFound)

	want := gofrhttp.ErrorEntityNotFound{Name: "id", Value: "9"}

	_, err := svc.Get(ctx, 5, 9)
	assert.Equal(t, want, err)
	assert.Equal(t, want, svc.Delete(ctx, 5, 9))
}
