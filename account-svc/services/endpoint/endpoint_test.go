package endpoint

import (
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/account-svc/common/apperr"
	"github.com/zopping/account-svc/filters"
	"github.com/zopping/account-svc/models"
	"github.com/zopping/account-svc/stores"
)

func invalid(param string) error { return gofrhttp.ErrorInvalidParam{Params: []string{param}} }

func setup(t *testing.T) (*Service, *stores.MockEndpoint, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	st := stores.NewMockEndpoint(ctrl)

	return New(st), st, &gofr.Context{}
}

func TestNormalisePath(t *testing.T) {
	good := map[string]string{
		"/product":                  "/product",
		"/product/":                 "/product",
		"/product/{id}":             "/product/{id}",
		"  /product/{id}/variant  ": "/product/{id}/variant",
		"/":                         "/",
		"/stock/{id}/price-log":     "/stock/{id}/price-log",
	}

	for in, want := range good {
		got, ok := NormalisePath(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}

	for _, bad := range []string{"", "product", "/product?x=1", "/prod uct", "/product/{id", "/product/id}", "/product/{}",
		"/product/{a}{b}", "/product/x{id}", "/product/{{id}}", "/p#frag"} {
		_, ok := NormalisePath(bad)
		assert.False(t, ok, bad)
	}
}

func TestCreate_Validation(t *testing.T) {
	svc, _, ctx := setup(t)

	_, err := svc.Create(ctx, &models.EndpointRequest{Service: " ", Method: "GET", Path: "/x"})
	assert.Equal(t, invalid("service"), err)

	_, err = svc.Create(ctx, &models.EndpointRequest{Service: "s", Method: "FETCH", Path: "/x"})
	assert.Equal(t, invalid("method"), err)

	_, err = svc.Create(ctx, &models.EndpointRequest{Service: "s", Method: "GET", Path: "x"})
	assert.Equal(t, invalid("path"), err)
}

func TestCreate_NormalisesAndMapsConflict(t *testing.T) {
	svc, st, ctx := setup(t)

	st.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ *gofr.Context, r *models.EndpointRequest) (models.Endpoint, error) {
		assert.Equal(t, "catalogue-svc", r.Service)
		assert.Equal(t, "GET", r.Method, "method upper-cased")
		assert.Equal(t, "/product/{id}", r.Path, "trailing slash dropped")

		return models.Endpoint{ID: 1}, nil
	})

	_, err := svc.Create(ctx, &models.EndpointRequest{Service: " catalogue-svc ", Method: "get", Path: "/product/{id}/"})
	assert.NoError(t, err)

	st.EXPECT().Create(ctx, gomock.Any()).Return(models.Endpoint{}, apperr.ErrConflict)

	_, err = svc.Create(ctx, &models.EndpointRequest{Service: "s", Method: "GET", Path: "/x"})
	assert.Equal(t, gofrhttp.ErrorEntityAlreadyExist{}, err)
}

func TestListGetUpdateDelete(t *testing.T) {
	svc, st, ctx := setup(t)

	_, err := svc.List(ctx, &filters.EndpointFilter{Method: "FETCH"})
	assert.Equal(t, invalid("method"), err)

	f := &filters.EndpointFilter{Method: "get", Limit: 20}
	st.EXPECT().List(ctx, f).Return([]models.Endpoint{{ID: 1}}, 1, nil)

	_, err = svc.List(ctx, f)
	assert.NoError(t, err)
	assert.Equal(t, "GET", f.Method, "the method filter is case-insensitive")

	st.EXPECT().Get(ctx, int64(404)).Return(models.Endpoint{}, apperr.ErrNotFound)

	_, err = svc.Get(ctx, 404)
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "404"}, err)

	st.EXPECT().Update(ctx, int64(1), gomock.Any()).Return(models.Endpoint{ID: 1}, nil)

	_, err = svc.Update(ctx, 1, &models.EndpointRequest{Service: "s", Method: "POST", Path: "/x"})
	assert.NoError(t, err)

	st.EXPECT().Delete(ctx, int64(1)).Return(nil)
	assert.NoError(t, svc.Delete(ctx, 1))
}
