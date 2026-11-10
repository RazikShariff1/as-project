package customer

import (
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/customer-svc/common/apperr"
	"github.com/zopping/customer-svc/common/password"
	"github.com/zopping/customer-svc/filters"
	"github.com/zopping/customer-svc/models"
	"github.com/zopping/customer-svc/stores"
)

func setup(t *testing.T) (*Service, *stores.MockCustomer, *password.Hasher, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	m, h := stores.NewMockCustomer(ctrl), password.New(4)

	return New(m, h), m, h, &gofr.Context{}
}

func TestNormaliseEmail(t *testing.T) {
	good := map[string]string{"A@Example.com": "a@example.com", "  b@x.io ": "b@x.io"}
	for in, want := range good {
		got, ok := NormaliseEmail(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got)
	}

	for _, in := range []string{"", "nope", "a@b", "Name <a@b.com>", "a@b.com, c@d.com"} {
		_, ok := NormaliseEmail(in)
		assert.False(t, ok, in)
	}
}

func TestNormaliseProfile(t *testing.T) {
	name, phone, err := NormaliseProfile("  Asha  ", " +91 (98765)-43210 ")
	assert.NoError(t, err)
	assert.Equal(t, "Asha", name)
	assert.Equal(t, "+91 (98765)-43210", phone)

	_, _, err = NormaliseProfile("", "")
	assert.Equal(t, gofrhttp.ErrorInvalidParam{Params: []string{"name"}}, err)

	_, _, err = NormaliseProfile("Asha", "12ab")
	assert.Equal(t, gofrhttp.ErrorInvalidParam{Params: []string{"phone"}}, err)

	_, _, err = NormaliseProfile("Asha", "")
	assert.NoError(t, err, "a phone number is optional")
}

func TestChangePassword(t *testing.T) {
	svc, m, h, ctx := setup(t)

	hash, err := h.Hash("old-password-1")
	assert.NoError(t, err)

	m.EXPECT().Get(ctx, int64(3)).Return(models.Customer{ID: 3, Email: "a@example.com"}, nil).Times(2)
	m.EXPECT().Credentials(ctx, "a@example.com").Return(models.Credentials{PasswordHash: hash}, nil).Times(2)
	m.EXPECT().SetPassword(ctx, int64(3), gomock.Any()).DoAndReturn(func(_ *gofr.Context, _ int64, newHash string) error {
		assert.NoError(t, h.Compare(newHash, "new-password-1"))

		return nil
	})

	assert.NoError(t, svc.ChangePassword(ctx, 3, &models.PasswordChangeRequest{CurrentPassword: "old-password-1", NewPassword: "new-password-1"}))

	var unauthorized *apperr.Unauthorized

	err = svc.ChangePassword(ctx, 3, &models.PasswordChangeRequest{CurrentPassword: "guess", NewPassword: "new-password-1"})
	assert.ErrorAs(t, err, &unauthorized, "the current password must be right; nothing is changed")

	err = svc.ChangePassword(ctx, 3, &models.PasswordChangeRequest{CurrentPassword: "old-password-1", NewPassword: "short"})
	assert.Equal(t, gofrhttp.ErrorInvalidParam{Params: []string{"newPassword"}}, err)
}

func TestSetStatus(t *testing.T) {
	svc, m, _, ctx := setup(t)

	m.EXPECT().SetStatus(ctx, int64(3), models.StatusSuspended).Return(models.Customer{ID: 3, Status: models.StatusSuspended}, nil)

	c, err := svc.SetStatus(ctx, 3, &models.StatusRequest{Status: models.StatusSuspended})
	assert.NoError(t, err)
	assert.Equal(t, models.StatusSuspended, c.Status)

	_, err = svc.SetStatus(ctx, 3, &models.StatusRequest{Status: "banned"})
	assert.Equal(t, gofrhttp.ErrorInvalidParam{Params: []string{"status"}}, err)

	m.EXPECT().SetStatus(ctx, int64(9), models.StatusActive).Return(models.Customer{}, apperr.ErrNotFound)

	_, err = svc.SetStatus(ctx, 9, &models.StatusRequest{Status: models.StatusActive})
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "9"}, err)
}

func TestList_RejectsAnUnknownStatusFilter(t *testing.T) {
	svc, m, _, ctx := setup(t)

	_, err := svc.List(ctx, &filters.CustomerFilter{Status: "banned"})
	assert.Equal(t, gofrhttp.ErrorInvalidParam{Params: []string{"status"}}, err)

	f := &filters.CustomerFilter{Limit: 20}
	m.EXPECT().List(ctx, f).Return([]models.Customer{{ID: 1}}, 1, nil)

	got, err := svc.List(ctx, f)
	assert.NoError(t, err)
	assert.Equal(t, 1, got.Count)
}
