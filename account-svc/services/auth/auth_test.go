package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/account-svc/common/apperr"
	"github.com/zopping/account-svc/common/authctx"
	"github.com/zopping/account-svc/common/password"
	"github.com/zopping/account-svc/models"
	"github.com/zopping/account-svc/stores"
)

const goodPassword = "correct-horse-1"

var fixedNow = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

type fixture struct {
	svc      *Service
	users    *stores.MockUser
	roles    *stores.MockRole
	sessions *stores.MockSession
	hasher   *password.Hasher
	ctx      *gofr.Context
}

// setup builds a service over mocks, with fast bcrypt, a fixed clock, and a context whose request carries client metadata.
func setup(t *testing.T) fixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	f := fixture{
		users:    stores.NewMockUser(ctrl),
		roles:    stores.NewMockRole(ctrl),
		sessions: stores.NewMockSession(ctrl),
		hasher:   password.New(4),
	}

	svc, err := New(f.users, f.roles, f.sessions, f.hasher, 3600)
	assert.NoError(t, err)

	svc.now = func() time.Time { return fixedNow }
	f.svc = svc

	r := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	r.Header.Set("User-Agent", "test-agent")
	r.Header.Set("X-Forwarded-For", "198.51.100.7")

	authctx.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		f.ctx = &gofr.Context{Request: gofrhttp.NewRequest(req)}
	})).ServeHTTP(httptest.NewRecorder(), r)

	return f
}

func (f fixture) creds(t *testing.T, status string) models.Credentials {
	t.Helper()

	hash, err := f.hasher.Hash(goodPassword)
	assert.NoError(t, err)

	return models.Credentials{User: models.User{ID: 1, Email: "a@example.com", Status: status}, PasswordHash: hash}
}

func TestLogin_Success(t *testing.T) {
	f := setup(t)
	login := &models.LoginRequest{Email: "  A@Example.COM ", Password: goodPassword}

	f.users.EXPECT().Credentials(f.ctx, "a@example.com").Return(f.creds(t, models.UserStatusActive), nil)
	f.users.EXPECT().RecordSuccess(f.ctx, int64(1)).Return(nil)

	var storedHash string

	f.sessions.EXPECT().Create(f.ctx, int64(1), gomock.Any(), 3600, "test-agent", "198.51.100.7").DoAndReturn(
		func(_ *gofr.Context, _ int64, hash string, _ int, _, _ string) (time.Time, error) {
			storedHash = hash

			return fixedNow.Add(time.Hour), nil
		})

	got, err := f.svc.Login(f.ctx, login)

	assert.NoError(t, err)
	assert.Len(t, got.Token, 43, "32 random bytes, base64url without padding")
	assert.Equal(t, HashToken(got.Token), storedHash, "the store receives the hash")
	assert.NotEqual(t, got.Token, storedHash, "the token itself is never stored")
	assert.Equal(t, fixedNow.Add(time.Hour), got.ExpiresAt)
	assert.Equal(t, int64(1), got.User.ID)
}

func TestLogin_TokensAreUnique(t *testing.T) {
	a, err := newToken()
	assert.NoError(t, err)

	b, err := newToken()
	assert.NoError(t, err)
	assert.NotEqual(t, a, b)
}

func TestLogin_UnknownOrMalformedLoginIs401AndTouchesNothing(t *testing.T) {
	f := setup(t)

	f.users.EXPECT().Credentials(f.ctx, "nobody@example.com").Return(models.Credentials{}, apperr.ErrNotFound)

	var unauthorized *apperr.Unauthorized

	_, err := f.svc.Login(f.ctx, &models.LoginRequest{Email: "nobody@example.com", Password: goodPassword})
	assert.ErrorAs(t, err, &unauthorized, "an unknown email looks exactly like a wrong password")
	assert.Equal(t, invalidCreds, unauthorized.Reason)

	for _, req := range []models.LoginRequest{
		{Email: "not-an-email", Password: goodPassword},
		{Email: "a@example.com", Password: ""},
		{Email: "", Password: ""},
	} {
		_, err := f.svc.Login(f.ctx, &req)
		assert.ErrorAs(t, err, &unauthorized, "%+v", req)
	}
}

func TestLogin_WrongPasswordCountsAndEventuallyLocks(t *testing.T) {
	f := setup(t)
	req := &models.LoginRequest{Email: "a@example.com", Password: "wrong-password"}

	f.users.EXPECT().Credentials(f.ctx, "a@example.com").Return(f.creds(t, models.UserStatusActive), nil).Times(2)
	f.users.EXPECT().RecordFailure(f.ctx, int64(1), maxFailures, lockSeconds).Return(false, nil)
	f.users.EXPECT().RecordFailure(f.ctx, int64(1), maxFailures, lockSeconds).Return(true, nil)

	var (
		unauthorized *apperr.Unauthorized
		locked       *apperr.Locked
	)

	_, err := f.svc.Login(f.ctx, req)
	assert.ErrorAs(t, err, &unauthorized)

	_, err = f.svc.Login(f.ctx, req)
	assert.ErrorAs(t, err, &locked, "the failure that reaches the limit reports the lock")
}

func TestLogin_LockedAccountIsRefusedEvenWithTheRightPassword(t *testing.T) {
	f := setup(t)
	c := f.creds(t, models.UserStatusActive)
	until := fixedNow.Add(10 * time.Minute)
	c.LockedUntil = &until

	f.users.EXPECT().Credentials(f.ctx, "a@example.com").Return(c, nil)

	var locked *apperr.Locked

	_, err := f.svc.Login(f.ctx, &models.LoginRequest{Email: "a@example.com", Password: goodPassword})
	assert.ErrorAs(t, err, &locked, "no password check, no session, no failure recorded")
}

func TestLogin_ExpiredLockIsClearedThenLoginProceeds(t *testing.T) {
	f := setup(t)
	c := f.creds(t, models.UserStatusActive)
	past := fixedNow.Add(-time.Minute)
	c.LockedUntil = &past

	f.users.EXPECT().Credentials(f.ctx, "a@example.com").Return(c, nil)
	f.users.EXPECT().ClearExpiredLock(f.ctx, int64(1)).Return(nil)
	f.users.EXPECT().RecordSuccess(f.ctx, int64(1)).Return(nil)
	f.sessions.EXPECT().Create(f.ctx, int64(1), gomock.Any(), 3600, gomock.Any(), gomock.Any()).Return(fixedNow.Add(time.Hour), nil)

	_, err := f.svc.Login(f.ctx, &models.LoginRequest{Email: "a@example.com", Password: goodPassword})
	assert.NoError(t, err)
}

func TestLogin_SuspendedIs403OnlyAfterTheCorrectPassword(t *testing.T) {
	f := setup(t)

	f.users.EXPECT().Credentials(f.ctx, "a@example.com").Return(f.creds(t, models.UserStatusSuspended), nil).Times(2)
	f.users.EXPECT().RecordFailure(f.ctx, int64(1), maxFailures, lockSeconds).Return(false, nil)

	var (
		forbidden    *apperr.Forbidden
		unauthorized *apperr.Unauthorized
	)

	_, err := f.svc.Login(f.ctx, &models.LoginRequest{Email: "a@example.com", Password: goodPassword})
	assert.ErrorAs(t, err, &forbidden)

	_, err = f.svc.Login(f.ctx, &models.LoginRequest{Email: "a@example.com", Password: "wrong-password"})
	assert.ErrorAs(t, err, &unauthorized, "a wrong password must not reveal that the account is suspended")
}

func TestLogin_StoreErrorsPassThrough(t *testing.T) {
	f := setup(t)
	boom := errors.New("db down")

	f.users.EXPECT().Credentials(f.ctx, "a@example.com").Return(models.Credentials{}, boom)

	_, err := f.svc.Login(f.ctx, &models.LoginRequest{Email: "a@example.com", Password: goodPassword})
	assert.ErrorIs(t, err, boom)
}

func TestAuthenticate(t *testing.T) {
	f := setup(t)

	var (
		unauthorized *apperr.Unauthorized
		forbidden    *apperr.Forbidden
	)

	_, err := f.svc.Authenticate(f.ctx, "  ")
	assert.ErrorAs(t, err, &unauthorized, "missing token")

	f.sessions.EXPECT().Lookup(f.ctx, HashToken("unknown")).Return(models.SessionLookup{}, apperr.ErrNotFound)

	_, err = f.svc.Authenticate(f.ctx, "unknown")
	assert.ErrorAs(t, err, &unauthorized, "unknown, expired and revoked tokens are all the same 401")

	f.sessions.EXPECT().Lookup(f.ctx, HashToken("tok")).Return(
		models.SessionLookup{User: models.User{ID: 1, Status: models.UserStatusSuspended}}, nil)

	_, err = f.svc.Authenticate(f.ctx, "tok")
	assert.ErrorAs(t, err, &forbidden, "a suspended user's live token stops working immediately")

	f.sessions.EXPECT().Lookup(f.ctx, HashToken("good")).Return(models.SessionLookup{
		ExpiresAt: fixedNow.Add(time.Hour),
		User:      models.User{ID: 1, Status: models.UserStatusActive, Attributes: map[string]any{"sellerId": float64(12)}},
	}, nil)

	f.roles.EXPECT().Permissions(f.ctx, int64(1), "", "").Return(true, nil, nil)

	p, err := f.svc.Authenticate(f.ctx, "good")
	assert.NoError(t, err)
	assert.Equal(t, float64(12), p.User.Attributes["sellerId"], "attributes reach the gateway")
	assert.Equal(t, fixedNow.Add(time.Hour), p.ExpiresAt)
	assert.True(t, p.AllAccess, "who-am-I reports whether the roles grant all access")
}

func (f fixture) liveSession() {
	f.sessions.EXPECT().Lookup(f.ctx, HashToken("tok")).Return(
		models.SessionLookup{User: models.User{ID: 7, Status: models.UserStatusActive}}, nil)
}

func TestAuthorize_Rules(t *testing.T) {
	f := setup(t)

	var forbidden *apperr.Forbidden

	f.liveSession()
	f.roles.EXPECT().Permissions(f.ctx, int64(7), "catalogue-svc", "GET").Return(true, nil, nil)

	p, err := f.svc.Authorize(f.ctx, "tok", "catalogue-svc", "get", "/anything/at/all")
	assert.NoError(t, err)
	assert.True(t, p.AllAccess, "an all-access role may call unregistered paths")

	f.liveSession()
	f.roles.EXPECT().Permissions(f.ctx, int64(7), "catalogue-svc", "GET").Return(false, []string{"/product", "/product/{id}"}, nil)

	_, err = f.svc.Authorize(f.ctx, "tok", " catalogue-svc ", "GET", "/product/42/?expand=variants")
	assert.NoError(t, err, "query string and trailing slash are ignored when matching")

	f.liveSession()
	f.roles.EXPECT().Permissions(f.ctx, int64(7), "catalogue-svc", "GET").Return(false, []string{"/product/{id}"}, nil)

	_, err = f.svc.Authorize(f.ctx, "tok", "catalogue-svc", "GET", "/product/42/variants")
	assert.ErrorAs(t, err, &forbidden)
	assert.Equal(t, "not permitted to call GET /product/42/variants on catalogue-svc", forbidden.Reason)

	f.liveSession()
	f.roles.EXPECT().Permissions(f.ctx, int64(7), "catalogue-svc", "DELETE").Return(false, []string{}, nil)

	_, err = f.svc.Authorize(f.ctx, "tok", "catalogue-svc", "DELETE", "/product/42")
	assert.ErrorAs(t, err, &forbidden, "no endpoints granted at all")
}

func TestAuthorize_BadInputAndBadTokenNeverReachPermissions(t *testing.T) {
	f := setup(t)

	_, err := f.svc.Authorize(f.ctx, "tok", "", "GET", "/x")
	assert.Equal(t, gofrhttp.ErrorInvalidParam{Params: []string{"service"}}, err)

	_, err = f.svc.Authorize(f.ctx, "tok", "svc", "FETCH", "/x")
	assert.Equal(t, gofrhttp.ErrorInvalidParam{Params: []string{"method"}}, err)

	_, err = f.svc.Authorize(f.ctx, "tok", "svc", "GET", "no-slash")
	assert.Equal(t, gofrhttp.ErrorInvalidParam{Params: []string{"path"}}, err)

	f.sessions.EXPECT().Lookup(f.ctx, HashToken("bad")).Return(models.SessionLookup{}, apperr.ErrNotFound)

	var unauthorized *apperr.Unauthorized

	_, err = f.svc.Authorize(f.ctx, "bad", "svc", "GET", "/x")
	assert.ErrorAs(t, err, &unauthorized, "authentication comes before the permission lookup")
}

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"/product", "/product", true},
		{"/product", "/products", false},
		{"/product", "/Product", false},
		{"/product/{id}", "/product/42", true},
		{"/product/{id}", "/product/", false},
		{"/product/{id}", "/product", false},
		{"/product/{id}", "/product/42/variants", false},
		{"/product/{id}/variant/{vid}", "/product/1/variant/2", true},
		{"/product/{id}/variant/{vid}", "/product/1/other/2", false},
		{"/stock/availability", "/stock/{id}", false},
		{"/stock/{id}", "/stock/availability", true},
		{"/", "/", true},
		{"/", "/x", false},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, Match(tc.pattern, tc.path), "%s vs %s", tc.pattern, tc.path)
	}
}

func TestRequestPath(t *testing.T) {
	tests := map[string]string{
		"/product":              "/product",
		"/product/":             "/product",
		"/product/42?a=b":       "/product/42",
		"/product/42/?a=b#frag": "/product/42",
		"/":                     "/",
	}

	for in, want := range tests {
		got, ok := requestPath(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}

	for _, bad := range []string{"", "product", "?x=1"} {
		_, ok := requestPath(bad)
		assert.False(t, ok, bad)
	}
}

func TestHashToken(t *testing.T) {
	assert.Equal(t, HashToken("abc"), HashToken("abc"))
	assert.NotEqual(t, HashToken("abc"), HashToken("abd"))
	assert.Len(t, HashToken("abc"), 64, "hex SHA-256")
	assert.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", HashToken("abc"), "known SHA-256 of abc")
}

func TestLogout(t *testing.T) {
	f := setup(t)

	var unauthorized *apperr.Unauthorized

	assert.ErrorAs(t, f.svc.Logout(f.ctx, ""), &unauthorized)

	f.sessions.EXPECT().Revoke(f.ctx, HashToken("tok")).Return(nil).Times(2)
	assert.NoError(t, f.svc.Logout(f.ctx, "tok"))
	assert.NoError(t, f.svc.Logout(f.ctx, "tok"), "logout is safe to repeat")
}

func TestBootstrap(t *testing.T) {
	f := setup(t)

	f.users.EXPECT().Count(f.ctx).Return(3, nil)

	created, err := f.svc.Bootstrap(f.ctx, "admin@example.com", goodPassword)
	assert.NoError(t, err)
	assert.False(t, created, "a no-op once any user exists")

	f.users.EXPECT().Count(f.ctx).Return(0, nil)

	_, err = f.svc.Bootstrap(f.ctx, "not-an-email", goodPassword)
	assert.Error(t, err)

	f.users.EXPECT().Count(f.ctx).Return(0, nil)

	_, err = f.svc.Bootstrap(f.ctx, "admin@example.com", "short")
	assert.Error(t, err)
}

func TestBootstrap_CreatesRoleAndAdmin(t *testing.T) {
	f := setup(t)

	f.users.EXPECT().Count(f.ctx).Return(0, nil)
	f.roles.EXPECT().List(f.ctx, gomock.Any()).Return([]models.Role{}, 0, nil)
	f.roles.EXPECT().Create(f.ctx, &models.RoleRequest{Name: "admin", Description: "Full access", AllAccess: true}).
		Return(models.Role{ID: 5, Name: "admin", AllAccess: true}, nil)
	f.users.EXPECT().Create(f.ctx, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, req *models.UserRequest, hash string) (models.User, error) {
			assert.Equal(t, "admin@example.com", req.Email, "stored lower-case")
			assert.Equal(t, []int64{5}, req.RoleIDs)
			assert.NoError(t, f.hasher.Compare(hash, goodPassword), "the password is stored hashed")

			return models.User{ID: 1}, nil
		})

	created, err := f.svc.Bootstrap(f.ctx, " Admin@Example.com ", goodPassword)
	assert.NoError(t, err)
	assert.True(t, created)
}

func TestBootstrap_ReusesAnExistingAdminRole(t *testing.T) {
	f := setup(t)

	f.users.EXPECT().Count(f.ctx).Return(0, nil)
	f.roles.EXPECT().List(f.ctx, gomock.Any()).Return([]models.Role{
		{ID: 2, Name: "administrators", AllAccess: true}, {ID: 9, Name: "admin", AllAccess: true},
	}, 2, nil)
	f.users.EXPECT().Create(f.ctx, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, req *models.UserRequest, _ string) (models.User, error) {
			assert.Equal(t, []int64{9}, req.RoleIDs, "the exact name match, not a prefix match")

			return models.User{ID: 1}, nil
		})

	created, err := f.svc.Bootstrap(f.ctx, "admin@example.com", goodPassword)
	assert.NoError(t, err)
	assert.True(t, created)
}
