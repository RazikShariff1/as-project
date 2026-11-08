package category

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

	svc := New(stores.NewMockCategory(ctrl))
	ctx := &gofr.Context{}

	_, err := svc.Create(ctx, &models.CategoryRequest{Name: ""})
	assert.Equal(t, invalid("name"), err)

	_, err = svc.Create(ctx, &models.CategoryRequest{Name: "Tea", Slug: "Not A Slug"})
	assert.Equal(t, invalid("slug"), err)

	_, err = svc.Create(ctx, &models.CategoryRequest{Name: "Tea", Status: "gone"})
	assert.Equal(t, invalid("status"), err)
}

func TestCreate_DefaultsAndStoreErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	st := stores.NewMockCategory(ctrl)
	svc := New(st)
	ctx := &gofr.Context{}

	st.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, r *models.CategoryRequest) (models.Category, error) {
			return models.Category{Slug: r.Slug, Status: r.Status}, nil
		})

	c, err := svc.Create(ctx, &models.CategoryRequest{Name: "Tea & Coffee"})
	assert.NoError(t, err)
	assert.Equal(t, "tea-coffee", c.Slug)
	assert.Equal(t, models.StatusEnabled, c.Status)

	st.EXPECT().Create(ctx, gomock.Any()).Return(models.Category{}, apperr.ErrConflict)

	_, err = svc.Create(ctx, &models.CategoryRequest{Name: "Tea"})
	assert.Equal(t, gofrhttp.ErrorEntityAlreadyExist{}, err)

	st.EXPECT().Create(ctx, gomock.Any()).Return(models.Category{}, &apperr.InvalidRef{Constraint: "categories_parent_category_id_fkey"})

	parent := int64(99)
	_, err = svc.Create(ctx, &models.CategoryRequest{Name: "Tea", ParentCategoryID: &parent})
	assert.Equal(t, invalid("parentCategoryId"), err, "unknown parent")
}

func TestUpdate_RejectsSelfParent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	svc := New(stores.NewMockCategory(ctrl))

	id := int64(5)
	_, err := svc.Update(&gofr.Context{}, id, &models.CategoryRequest{Name: "Tea", ParentCategoryID: &id})
	assert.Equal(t, invalid("parentCategoryId"), err)
}

func TestListAndDelete(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	st := stores.NewMockCategory(ctrl)
	svc := New(st)
	ctx := &gofr.Context{}

	_, err := svc.List(ctx, &filters.CategoryFilter{Status: "gone"})
	assert.Equal(t, invalid("status"), err)

	f := &filters.CategoryFilter{Limit: 20}
	st.EXPECT().List(ctx, f).Return([]models.Category{{ID: 1}}, 1, nil)

	got, err := svc.List(ctx, f)
	assert.NoError(t, err)
	assert.Equal(t, 1, got.Count)

	st.EXPECT().Delete(ctx, int64(404)).Return(apperr.ErrNotFound)
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "404"}, svc.Delete(ctx, 404))
}
