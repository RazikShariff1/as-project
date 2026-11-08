package product

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

type fixture struct {
	svc      *Service
	products *stores.MockProduct
	variants *stores.MockVariant
	ctx      *gofr.Context
}

func setup(t *testing.T) fixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	f := fixture{
		products: stores.NewMockProduct(ctrl),
		variants: stores.NewMockVariant(ctrl),
		ctx:      &gofr.Context{},
	}
	f.svc = New(f.products, f.variants)

	return f
}

func invalid(param string) error { return gofrhttp.ErrorInvalidParam{Params: []string{param}} }

func TestCreate_Validation(t *testing.T) {
	f := setup(t)

	_, err := f.svc.Create(f.ctx, &models.ProductRequest{Name: " "})
	assert.Equal(t, invalid("name"), err)

	_, err = f.svc.Create(f.ctx, &models.ProductRequest{Name: "Tea", Status: "bogus"})
	assert.Equal(t, invalid("status"), err)
}

func TestCreate_DefaultsAndGeneratedSlug(t *testing.T) {
	f := setup(t)

	f.products.EXPECT().Create(f.ctx, gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, r *models.ProductRequest) (models.Product, error) {
			return models.Product{ID: 1, Status: r.Status, Slug: r.Slug}, nil
		})

	p, err := f.svc.Create(f.ctx, &models.ProductRequest{Name: "Green Tea 100g"})

	assert.NoError(t, err)
	assert.Equal(t, models.StatusEnabled, p.Status)
	assert.Equal(t, "green-tea-100g", p.Slug)
}

func TestCreate_UnknownCategory(t *testing.T) {
	f := setup(t)

	f.products.EXPECT().Create(f.ctx, gomock.Any()).Return(models.Product{}, &apperr.InvalidRef{Constraint: "products_primary_category_id_fkey"})

	_, err := f.svc.Create(f.ctx, &models.ProductRequest{Name: "Tea"})
	assert.Equal(t, invalid("primaryCategoryId"), err)

	f.products.EXPECT().Create(f.ctx, gomock.Any()).Return(models.Product{}, &apperr.InvalidRef{Constraint: "products_brand_id_fkey"})

	_, err = f.svc.Create(f.ctx, &models.ProductRequest{Name: "Tea"})
	assert.Equal(t, invalid("brandId"), err)

	f.products.EXPECT().Create(f.ctx, gomock.Any()).Return(models.Product{}, apperr.ErrConflict)

	_, err = f.svc.Create(f.ctx, &models.ProductRequest{Name: "Tea"})
	assert.Equal(t, gofrhttp.ErrorEntityAlreadyExist{}, err, "duplicate slug")
}

func TestList_AttachesVariants(t *testing.T) {
	f := setup(t)
	filter := &filters.ProductFilter{Limit: 20}

	f.products.EXPECT().List(f.ctx, filter).Return([]models.Product{{ID: 1}, {ID: 2}}, 7, nil)
	f.variants.EXPECT().ListByProductIDs(f.ctx, []int64{1, 2}).Return([]models.Variant{
		{ID: 1, ProductID: 2}, {ID: 2, ProductID: 2}, {ID: 3, ProductID: 1},
	}, nil)

	got, err := f.svc.List(f.ctx, filter)

	assert.NoError(t, err)
	assert.Equal(t, 7, got.Count)
	assert.Equal(t, 20, got.Limit)
	assert.Len(t, got.Products[0].Variants, 1)
	assert.Len(t, got.Products[1].Variants, 2)
}

func TestList_RejectsBadStatusAndSkipsVariantQueryWhenEmpty(t *testing.T) {
	f := setup(t)

	_, err := f.svc.List(f.ctx, &filters.ProductFilter{Status: "bogus"})
	assert.Equal(t, invalid("status"), err)

	filter := &filters.ProductFilter{Limit: 20}
	f.products.EXPECT().List(f.ctx, filter).Return([]models.Product{}, 0, nil)

	got, err := f.svc.List(f.ctx, filter)
	assert.NoError(t, err)
	assert.Empty(t, got.Products)
}

func TestGetAndDelete_NotFound(t *testing.T) {
	f := setup(t)

	f.products.EXPECT().Get(f.ctx, int64(999)).Return(models.Product{}, apperr.ErrNotFound)

	_, err := f.svc.Get(f.ctx, 999)
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "999"}, err)

	f.products.EXPECT().Delete(f.ctx, int64(999)).Return(apperr.ErrNotFound)
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "999"}, f.svc.Delete(f.ctx, 999))
}

func TestNormalise_MergesPrimaryIntoCategoryIDs(t *testing.T) {
	primary := int64(3)
	req := &models.ProductRequest{Name: "Tea", PrimaryCategoryID: &primary, CategoryIDs: []int64{5, 3, 5, 7}}

	assert.NoError(t, normalise(req))
	assert.Equal(t, []int64{3, 5, 7}, req.CategoryIDs, "primary first, no duplicates")

	req = &models.ProductRequest{Name: "Tea"}
	assert.NoError(t, normalise(req))
	assert.Empty(t, req.CategoryIDs)

	bad := int64(0)
	assert.Equal(t, invalid("primaryCategoryId"), normalise(&models.ProductRequest{Name: "Tea", PrimaryCategoryID: &bad}))
	assert.Equal(t, invalid("categoryIds"), normalise(&models.ProductRequest{Name: "Tea", CategoryIDs: []int64{1, -2}}))
}

func TestNormalise_RejectsBadSlug(t *testing.T) {
	assert.Equal(t, invalid("slug"), normalise(&models.ProductRequest{Name: "T", Slug: "Bad Slug"}))
	assert.Equal(t, invalid("slug"), normalise(&models.ProductRequest{Name: "!!!"}), "no letters or digits to build a slug from")
}

func TestGetAndUpdate_ReturnVariants(t *testing.T) {
	f := setup(t)
	variants := []models.Variant{{ID: 10, ProductID: 1}, {ID: 11, ProductID: 1}}

	f.products.EXPECT().Get(f.ctx, int64(1)).Return(models.Product{ID: 1}, nil)
	f.variants.EXPECT().ListByProductIDs(f.ctx, []int64{1}).Return(variants, nil)

	got, err := f.svc.Get(f.ctx, 1)
	assert.NoError(t, err)
	assert.Equal(t, variants, got.Variants, "Get must include the product's variants")

	f.products.EXPECT().Update(f.ctx, int64(1), gomock.Any()).Return(models.Product{ID: 1}, nil)
	f.variants.EXPECT().ListByProductIDs(f.ctx, []int64{1}).Return(variants, nil)

	got, err = f.svc.Update(f.ctx, 1, &models.ProductRequest{Name: "Tea"})
	assert.NoError(t, err)
	assert.Equal(t, variants, got.Variants, "Update must include the product's variants")
}
