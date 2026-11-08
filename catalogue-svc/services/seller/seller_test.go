package seller

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/catalogue-svc/common/apperr"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/stores"
)

func TestService_Create(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := &gofr.Context{}
	st := stores.NewMockSeller(ctrl)
	svc := New(st)

	tests := []struct {
		name    string
		req     models.SellerRequest
		storeFn func()
		wantErr error
	}{
		{name: "blank name", req: models.SellerRequest{Name: "  "}, wantErr: gofrhttp.ErrorInvalidParam{Params: []string{"name"}}},
		{
			name: "duplicate house seller", req: models.SellerRequest{Name: "Zopping", IsHouse: true},
			storeFn: func() { st.EXPECT().Create(ctx, gomock.Any()).Return(models.Seller{}, apperr.ErrConflict) },
			wantErr: gofrhttp.ErrorEntityAlreadyExist{},
		},
		{
			name: "success", req: models.SellerRequest{Name: "Acme"},
			storeFn: func() { st.EXPECT().Create(ctx, gomock.Any()).Return(models.Seller{ID: 1, Name: "Acme"}, nil) },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.storeFn != nil {
				tc.storeFn()
			}

			got, err := svc.Create(ctx, &tc.req)

			assert.Equal(t, tc.wantErr, err)

			if tc.wantErr == nil {
				assert.Equal(t, "Acme", got.Name)
			}
		})
	}
}

func TestService_Update(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := &gofr.Context{}
	st := stores.NewMockSeller(ctrl)
	svc := New(st)

	_, err := svc.Update(ctx, 1, &models.SellerRequest{Name: "", Status: models.SellerStatusActive})
	assert.Equal(t, gofrhttp.ErrorInvalidParam{Params: []string{"name"}}, err)

	_, err = svc.Update(ctx, 1, &models.SellerRequest{Name: "Acme", Status: "banned"})
	assert.Equal(t, gofrhttp.ErrorInvalidParam{Params: []string{"status"}}, err)

	st.EXPECT().Update(ctx, int64(404), gomock.Any()).Return(models.Seller{}, apperr.ErrNotFound)

	_, err = svc.Update(ctx, 404, &models.SellerRequest{Name: "Acme", Status: models.SellerStatusActive})
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "404"}, err)
}

func TestService_ListAndGet(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := &gofr.Context{}
	st := stores.NewMockSeller(ctrl)
	svc := New(st)

	_, err := svc.List(ctx, &filters.SellerFilter{Status: "banned"})
	assert.Equal(t, gofrhttp.ErrorInvalidParam{Params: []string{"status"}}, err)

	st.EXPECT().List(ctx, gomock.Any()).Return([]models.Seller{{ID: 1}}, nil)

	list, err := svc.List(ctx, &filters.SellerFilter{Status: models.SellerStatusActive, Limit: 20})
	assert.NoError(t, err)
	assert.Len(t, list, 1)

	boom := errors.New("db down")
	st.EXPECT().Get(ctx, int64(1)).Return(models.Seller{}, boom)

	_, err = svc.Get(ctx, 1)
	assert.ErrorIs(t, err, boom)
}
