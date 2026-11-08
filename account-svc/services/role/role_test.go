package role

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

func setup(t *testing.T) (*Service, *stores.MockRole, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	st := stores.NewMockRole(ctrl)

	return New(st), st, &gofr.Context{}
}

func TestCreate_Validation(t *testing.T) {
	svc, _, ctx := setup(t)

	_, err := svc.Create(ctx, &models.RoleRequest{Name: "  "})
	assert.Equal(t, invalid("name"), err)

	_, err = svc.Create(ctx, &models.RoleRequest{Name: "seller", EndpointIDs: []int64{1, -4}})
	assert.Equal(t, invalid("endpointIds"), err)
}

func TestCreate_DedupesEndpointsAndMapsErrors(t *testing.T) {
	svc, st, ctx := setup(t)

	st.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ *gofr.Context, r *models.RoleRequest) (models.Role, error) {
		assert.Equal(t, "seller", r.Name)
		assert.Equal(t, []int64{4, 2}, r.EndpointIDs)

		return models.Role{ID: 1}, nil
	})

	_, err := svc.Create(ctx, &models.RoleRequest{Name: " seller ", EndpointIDs: []int64{4, 2, 4}})
	assert.NoError(t, err)

	st.EXPECT().Create(ctx, gomock.Any()).Return(models.Role{}, apperr.ErrConflict)

	_, err = svc.Create(ctx, &models.RoleRequest{Name: "seller"})
	assert.Equal(t, gofrhttp.ErrorEntityAlreadyExist{}, err, "duplicate role name")

	st.EXPECT().Create(ctx, gomock.Any()).Return(models.Role{}, &apperr.InvalidRef{Constraint: "endpoint_role_endpoint_id_fkey"})

	_, err = svc.Create(ctx, &models.RoleRequest{Name: "seller", EndpointIDs: []int64{999}})
	assert.Equal(t, invalid("endpointIds"), err, "unknown or deleted endpoint")
}

func TestListGetUpdateDelete(t *testing.T) {
	svc, st, ctx := setup(t)

	f := &filters.RoleFilter{Limit: 20}
	st.EXPECT().List(ctx, f).Return([]models.Role{{ID: 1}}, 1, nil)

	got, err := svc.List(ctx, f)
	assert.NoError(t, err)
	assert.Equal(t, 1, got.Count)

	st.EXPECT().Get(ctx, int64(404)).Return(models.Role{}, apperr.ErrNotFound)

	_, err = svc.Get(ctx, 404)
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "404"}, err)

	st.EXPECT().Update(ctx, int64(1), gomock.Any()).Return(models.Role{ID: 1}, nil)

	_, err = svc.Update(ctx, 1, &models.RoleRequest{Name: "seller"})
	assert.NoError(t, err)

	st.EXPECT().Delete(ctx, int64(1)).Return(nil)
	assert.NoError(t, svc.Delete(ctx, 1))
}
