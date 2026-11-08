package variant

import (
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

func invalid(param string) error { return gofrhttp.ErrorInvalidParam{Params: []string{param}} }

func TestCreate_Validation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	svc := New(stores.NewMockVariant(ctrl))
	ctx := &gofr.Context{}
	valid := models.VariantRequest{ProductID: 1, ClientItemID: "SKU-1"}

	tests := []struct {
		name   string
		mutate func(r *models.VariantRequest)
		want   string
	}{
		{"missing product", func(r *models.VariantRequest) { r.ProductID = 0 }, "productId"},
		{"blank sku", func(r *models.VariantRequest) { r.ClientItemID = " " }, "clientItemId"},
		{"bad status", func(r *models.VariantRequest) { r.Status = "bogus" }, "status"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := valid
			tc.mutate(&req)

			_, err := svc.Create(ctx, &req)
			assert.Equal(t, invalid(tc.want), err)
		})
	}
}

func TestCreate_DefaultsAndStoreErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	st := stores.NewMockVariant(ctrl)
	svc := New(st)
	ctx := &gofr.Context{}
	req := func() *models.VariantRequest {
		return &models.VariantRequest{ProductID: 1, ClientItemID: "SKU-1"}
	}

	st.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, r *models.VariantRequest) (models.Variant, error) {
			return models.Variant{Status: r.Status}, nil
		})

	v, err := svc.Create(ctx, req())
	assert.NoError(t, err)
	assert.Equal(t, models.StatusEnabled, v.Status)

	st.EXPECT().Create(ctx, gomock.Any()).Return(models.Variant{}, apperr.ErrConflict)

	_, err = svc.Create(ctx, req())
	assert.Equal(t, gofrhttp.ErrorEntityAlreadyExist{}, err, "duplicate sku on the same product")

	st.EXPECT().Create(ctx, gomock.Any()).Return(models.Variant{}, &apperr.InvalidRef{Constraint: "variants_product_id_fkey"})

	_, err = svc.Create(ctx, req())
	assert.Equal(t, invalid("productId"), err, "unknown product")
}

func TestListUpdateDelete(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	st := stores.NewMockVariant(ctrl)
	svc := New(st)
	ctx := &gofr.Context{}

	_, err := svc.List(ctx, &filters.VariantFilter{Status: "bogus"})
	assert.Equal(t, invalid("status"), err)

	f := &filters.VariantFilter{Limit: 20, Offset: 40}
	st.EXPECT().List(ctx, f).Return([]models.Variant{{ID: 1}}, 41, nil)

	got, err := svc.List(ctx, f)
	assert.NoError(t, err)
	assert.Equal(t, models.VariantList{Variants: []models.Variant{{ID: 1}}, Count: 41, Offset: 40, Limit: 20}, got)

	st.EXPECT().Update(ctx, int64(404), gomock.Any()).Return(models.Variant{}, apperr.ErrNotFound)

	_, err = svc.Update(ctx, 404, &models.VariantRequest{ClientItemID: "SKU-1"})
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "404"}, err)

	st.EXPECT().Delete(ctx, int64(1)).Return(nil)
	assert.NoError(t, svc.Delete(ctx, 1))
}
