package brand

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

	svc := New(stores.NewMockBrand(ctrl))
	ctx := &gofr.Context{}

	_, err := svc.Create(ctx, &models.BrandRequest{Name: "  "})
	assert.Equal(t, invalid("name"), err)

	_, err = svc.Create(ctx, &models.BrandRequest{Name: "!!!"})
	assert.Equal(t, invalid("slug"), err, "a name with no letters or digits cannot generate a slug")

	_, err = svc.Create(ctx, &models.BrandRequest{Name: "Acme", Slug: "Not A Slug"})
	assert.Equal(t, invalid("slug"), err)

	_, err = svc.Create(ctx, &models.BrandRequest{Name: "Acme", Status: "gone"})
	assert.Equal(t, invalid("status"), err)
}

func TestCreate_GeneratesSlugAndDefaultsStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	st := stores.NewMockBrand(ctrl)
	svc := New(st)
	ctx := &gofr.Context{}

	st.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, r *models.BrandRequest) (models.Brand, error) {
			return models.Brand{Slug: r.Slug, Status: r.Status}, nil
		})

	b, err := svc.Create(ctx, &models.BrandRequest{Name: "Acme Foods"})

	assert.NoError(t, err)
	assert.Equal(t, "acme-foods", b.Slug)
	assert.Equal(t, models.StatusEnabled, b.Status)

	st.EXPECT().Create(ctx, gomock.Any()).Return(models.Brand{}, apperr.ErrConflict)

	_, err = svc.Create(ctx, &models.BrandRequest{Name: "Acme Foods"})
	assert.Equal(t, gofrhttp.ErrorEntityAlreadyExist{}, err, "duplicate slug")
}

func TestListGetUpdateDelete(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	st := stores.NewMockBrand(ctrl)
	svc := New(st)
	ctx := &gofr.Context{}

	_, err := svc.List(ctx, &filters.BrandFilter{Status: "gone"})
	assert.Equal(t, invalid("status"), err)

	f := &filters.BrandFilter{Limit: 20, Offset: 20}
	st.EXPECT().List(ctx, f).Return([]models.Brand{{ID: 1}}, 21, nil)

	got, err := svc.List(ctx, f)
	assert.NoError(t, err)
	assert.Equal(t, models.BrandList{Brands: []models.Brand{{ID: 1}}, Count: 21, Offset: 20, Limit: 20}, got)

	st.EXPECT().Get(ctx, int64(404)).Return(models.Brand{}, apperr.ErrNotFound)

	_, err = svc.Get(ctx, 404)
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "404"}, err)

	st.EXPECT().Update(ctx, int64(404), gomock.Any()).Return(models.Brand{}, apperr.ErrNotFound)

	_, err = svc.Update(ctx, 404, &models.BrandRequest{Name: "Acme"})
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "404"}, err)

	st.EXPECT().Delete(ctx, int64(1)).Return(nil)
	assert.NoError(t, svc.Delete(ctx, 1))
}
