package permissible

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/catalogue-svc/common/apperr"
	"github.com/zopping/catalogue-svc/common/caller"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/stores"
)

func invalid(param string) error { return gofrhttp.ErrorInvalidParam{Params: []string{param}} }

func setup(t *testing.T) (*Service, *stores.MockPermissibleSeller, *stores.MockProduct, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	st, pr := stores.NewMockPermissibleSeller(ctrl), stores.NewMockProduct(ctrl)

	return New(st, pr), st, pr, &gofr.Context{}
}

func TestReplace_Validation(t *testing.T) {
	svc, _, _, ctx := setup(t)

	_, err := svc.Replace(ctx, 3, &models.PermissibleSellersRequest{SellerIDs: []int64{1, 0}})
	assert.Equal(t, invalid("sellerIds"), err)
}

func TestReplace_DedupesAndReturnsTheWholeList(t *testing.T) {
	svc, st, pr, ctx := setup(t)

	pr.EXPECT().Get(ctx, int64(3)).Return(models.Product{ID: 3}, nil)
	st.EXPECT().Replace(ctx, int64(3), []int64{5, 2}).Return(nil)
	st.EXPECT().List(ctx, gomock.Any()).Return([]models.PermissibleSeller{
		{SellerID: 2, ProductID: 3, Status: "ENABLED"}, {SellerID: 5, ProductID: 3, Status: "ENABLED"}, {SellerID: 8, ProductID: 3, Status: "DISABLED"},
	}, 3, nil)

	got, err := svc.Replace(ctx, 3, &models.PermissibleSellersRequest{SellerIDs: []int64{5, 2, 5}})
	assert.NoError(t, err)
	assert.Len(t, got, 3, "the caller sees every seller listed for the product, including the ones just disabled")
}

func TestReplace_Errors(t *testing.T) {
	svc, st, pr, ctx := setup(t)

	pr.EXPECT().Get(ctx, int64(404)).Return(models.Product{}, apperr.ErrNotFound)

	_, err := svc.Replace(ctx, 404, &models.PermissibleSellersRequest{SellerIDs: []int64{1}})
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "404"}, err)

	pr.EXPECT().Get(ctx, int64(3)).Return(models.Product{ID: 3}, nil)
	st.EXPECT().Replace(ctx, int64(3), []int64{999}).Return(&apperr.InvalidRef{Constraint: "permissible_product_sellers_seller_id_fkey"})

	_, err = svc.Replace(ctx, 3, &models.PermissibleSellersRequest{SellerIDs: []int64{999}})
	assert.Equal(t, invalid("sellerIds"), err, "an unknown seller")
}

func TestList(t *testing.T) {
	svc, st, _, ctx := setup(t)

	_, err := svc.List(ctx, &filters.PermissibleSellerFilter{Status: "MAYBE"})
	assert.Equal(t, invalid("status"), err)

	f := &filters.PermissibleSellerFilter{ProductID: 3, Limit: 20}
	st.EXPECT().List(ctx, f).Return([]models.PermissibleSeller{{SellerID: 1}}, 1, nil)

	got, err := svc.List(ctx, f)
	assert.NoError(t, err)
	assert.Equal(t, 1, got.Count)
}

func sellerCtx(id string) *gofr.Context {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("X-Seller-Id", id)

	var ctx *gofr.Context

	caller.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		ctx = &gofr.Context{Request: gofrhttp.NewRequest(req)}
	})).ServeHTTP(httptest.NewRecorder(), r)

	return ctx
}

func TestASellerCannotChangeTheWhitelistAndSeesOnlyTheirOwnEntries(t *testing.T) {
	svc, st, _, _ := setup(t)
	ctx := sellerCtx("7")

	var forbidden *apperr.Forbidden

	_, err := svc.Replace(ctx, 3, &models.PermissibleSellersRequest{SellerIDs: []int64{7}})
	assert.ErrorAs(t, err, &forbidden, "a seller cannot whitelist themselves")

	st.EXPECT().List(ctx, &filters.PermissibleSellerFilter{SellerID: 7, ProductID: 3, Limit: 20}).Return([]models.PermissibleSeller{}, 0, nil)

	_, err = svc.List(ctx, &filters.PermissibleSellerFilter{SellerID: 8, ProductID: 3, Limit: 20})
	assert.NoError(t, err, "asking about seller 8 still returns seller 7's entries")
}
