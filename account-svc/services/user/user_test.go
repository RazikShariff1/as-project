package user

import (
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/account-svc/common/apperr"
	"github.com/zopping/account-svc/common/password"
	"github.com/zopping/account-svc/filters"
	"github.com/zopping/account-svc/models"
	"github.com/zopping/account-svc/stores"
)

const goodPassword = "correct-horse-1"

func invalid(param string) error { return gofrhttp.ErrorInvalidParam{Params: []string{param}} }

type fixture struct {
	svc      *Service
	users    *stores.MockUser
	sessions *stores.MockSession
	hasher   *password.Hasher
	ctx      *gofr.Context
}

func setup(t *testing.T) fixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	f := fixture{
		users: stores.NewMockUser(ctrl), sessions: stores.NewMockSession(ctrl), hasher: password.New(4), ctx: &gofr.Context{},
	}
	f.svc = New(f.users, f.sessions, f.hasher)

	return f
}

func TestNormaliseEmail(t *testing.T) {
	good := map[string]string{
		"a@example.com":            "a@example.com",
		"  A.B+tag@Example.CO.IN ": "a.b+tag@example.co.in",
	}

	for in, want := range good {
		got, ok := NormaliseEmail(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}

	for _, bad := range []string{"", "plain", "a@", "@example.com", "a@localhost", "Name <a@example.com>", "a b@example.com",
		"a@example.com, b@example.com", strings.Repeat("a", 250) + "@example.com"} {
		_, ok := NormaliseEmail(bad)
		assert.False(t, ok, bad)
	}
}

func TestCreate_Validation(t *testing.T) {
	f := setup(t)
	valid := models.UserRequest{Name: "Asha", Email: "asha@example.com", Password: goodPassword}

	tests := []struct {
		name   string
		mutate func(r *models.UserRequest)
		want   string
	}{
		{"blank name", func(r *models.UserRequest) { r.Name = "  " }, "name"},
		{"bad email", func(r *models.UserRequest) { r.Email = "nope" }, "email"},
		{"short password", func(r *models.UserRequest) { r.Password = "short" }, "password"},
		{"long password", func(r *models.UserRequest) { r.Password = strings.Repeat("x", 73) }, "password"},
		{"bad status", func(r *models.UserRequest) { r.Status = "banned" }, "status"},
		{"bad role id", func(r *models.UserRequest) { r.RoleIDs = []int64{1, 0} }, "roleIds"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := valid
			tc.mutate(&req)

			_, err := f.svc.Create(f.ctx, &req)
			assert.Equal(t, invalid(tc.want), err)
		})
	}
}

func TestCreate_HashesThePasswordAndNormalises(t *testing.T) {
	f := setup(t)

	f.users.EXPECT().Create(f.ctx, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, req *models.UserRequest, hash string) (models.User, error) {
			assert.Equal(t, "asha@example.com", req.Email)
			assert.Equal(t, "Asha", req.Name)
			assert.Equal(t, models.UserStatusActive, req.Status, "status defaults to active")
			assert.Equal(t, []int64{3, 1}, req.RoleIDs, "duplicate roles removed, order kept")
			assert.NotEqual(t, goodPassword, hash)
			assert.NoError(t, f.hasher.Compare(hash, goodPassword))

			return models.User{ID: 1, Email: req.Email}, nil
		})

	u, err := f.svc.Create(f.ctx, &models.UserRequest{
		Name: " Asha ", Email: " Asha@Example.com ", Password: goodPassword, RoleIDs: []int64{3, 1, 3},
	})
	assert.NoError(t, err)
	assert.Equal(t, int64(1), u.ID)
}

func TestCreate_StoreErrors(t *testing.T) {
	f := setup(t)
	req := func() *models.UserRequest {
		return &models.UserRequest{Name: "Asha", Email: "asha@example.com", Password: goodPassword, RoleIDs: []int64{9}}
	}

	f.users.EXPECT().Create(f.ctx, gomock.Any(), gomock.Any()).Return(models.User{}, apperr.ErrConflict)

	_, err := f.svc.Create(f.ctx, req())
	assert.Equal(t, gofrhttp.ErrorEntityAlreadyExist{}, err, "duplicate email")

	f.users.EXPECT().Create(f.ctx, gomock.Any(), gomock.Any()).Return(models.User{}, &apperr.InvalidRef{Constraint: "user_role_role_id_fkey"})

	_, err = f.svc.Create(f.ctx, req())
	assert.Equal(t, invalid("roleIds"), err, "unknown or deleted role")
}

func TestUpdate_IgnoresThePassword(t *testing.T) {
	f := setup(t)

	f.users.EXPECT().Update(f.ctx, int64(4), gomock.Any()).Return(models.User{ID: 4}, nil)

	_, err := f.svc.Update(f.ctx, 4, &models.UserRequest{Name: "Asha", Email: "asha@example.com", Password: "ignored"})
	assert.NoError(t, err, "a password in an update body is not validated or used")

	f.users.EXPECT().Update(f.ctx, int64(404), gomock.Any()).Return(models.User{}, apperr.ErrNotFound)

	_, err = f.svc.Update(f.ctx, 404, &models.UserRequest{Name: "Asha", Email: "asha@example.com"})
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "404"}, err)
}

func TestChangePassword(t *testing.T) {
	f := setup(t)
	hash, _ := f.hasher.Hash(goodPassword)

	assert.Equal(t, invalid("newPassword"),
		f.svc.ChangePassword(f.ctx, 1, &models.PasswordChangeRequest{CurrentPassword: goodPassword, NewPassword: "short"}))

	f.users.EXPECT().Get(f.ctx, int64(1)).Return(models.User{ID: 1, Email: "a@example.com"}, nil).Times(2)
	f.users.EXPECT().Credentials(f.ctx, "a@example.com").Return(models.Credentials{PasswordHash: hash}, nil).Times(2)

	assert.Equal(t, invalid("currentPassword"),
		f.svc.ChangePassword(f.ctx, 1, &models.PasswordChangeRequest{CurrentPassword: "wrong-password", NewPassword: "brand-new-pass"}),
		"a wrong current password changes nothing")

	f.users.EXPECT().SetPassword(f.ctx, int64(1), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, _ int64, newHash string) error {
			assert.NoError(t, f.hasher.Compare(newHash, "brand-new-pass"))

			return nil
		})

	assert.NoError(t, f.svc.ChangePassword(f.ctx, 1, &models.PasswordChangeRequest{CurrentPassword: goodPassword, NewPassword: "brand-new-pass"}))
}

func TestResetPasswordAndRevokeSessions(t *testing.T) {
	f := setup(t)

	assert.Equal(t, invalid("newPassword"), f.svc.ResetPassword(f.ctx, 1, &models.PasswordResetRequest{NewPassword: "x"}))

	f.users.EXPECT().SetPassword(f.ctx, int64(1), gomock.Any()).Return(nil)
	assert.NoError(t, f.svc.ResetPassword(f.ctx, 1, &models.PasswordResetRequest{NewPassword: "brand-new-pass"}))

	f.users.EXPECT().SetPassword(f.ctx, int64(404), gomock.Any()).Return(apperr.ErrNotFound)
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "404"},
		f.svc.ResetPassword(f.ctx, 404, &models.PasswordResetRequest{NewPassword: "brand-new-pass"}))

	f.users.EXPECT().Get(f.ctx, int64(1)).Return(models.User{ID: 1}, nil)
	f.sessions.EXPECT().RevokeUser(f.ctx, int64(1)).Return(nil)
	assert.NoError(t, f.svc.RevokeSessions(f.ctx, 1))

	f.users.EXPECT().Get(f.ctx, int64(404)).Return(models.User{}, apperr.ErrNotFound)
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "404"}, f.svc.RevokeSessions(f.ctx, 404))
}

func TestListGetDelete(t *testing.T) {
	f := setup(t)

	_, err := f.svc.List(f.ctx, &filters.UserFilter{Status: "banned"})
	assert.Equal(t, invalid("status"), err)

	flt := &filters.UserFilter{Limit: 20}
	f.users.EXPECT().List(f.ctx, flt).Return([]models.User{{ID: 1}}, 1, nil)

	got, err := f.svc.List(f.ctx, flt)
	assert.NoError(t, err)
	assert.Equal(t, models.UserList{Users: []models.User{{ID: 1}}, Count: 1, Limit: 20}, got)

	f.users.EXPECT().Get(f.ctx, int64(2)).Return(models.User{ID: 2}, nil)

	u, err := f.svc.Get(f.ctx, 2)
	assert.NoError(t, err)
	assert.Equal(t, int64(2), u.ID)

	f.users.EXPECT().Delete(f.ctx, int64(2)).Return(nil)
	assert.NoError(t, f.svc.Delete(f.ctx, 2))
}
