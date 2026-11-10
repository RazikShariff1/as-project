package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/customer-svc/common/apperr"
	"github.com/zopping/customer-svc/common/authctx"
	"github.com/zopping/customer-svc/common/password"
	"github.com/zopping/customer-svc/models"
	"github.com/zopping/customer-svc/stores"
)

const goodPassword = "correct-horse-1"

var fixedNow = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

type fixture struct {
	svc       *Service
	customers *stores.MockCustomer
	sessions  *stores.MockSession
	hasher    *password.Hasher
	ctx       *gofr.Context
}

// setup builds a service over mocks, with fast bcrypt, a fixed clock, and a context whose request carries client metadata.
func setup(t *testing.T) fixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	f := fixture{customers: stores.NewMockCustomer(ctrl), sessions: stores.NewMockSession(ctrl), hasher: password.New(4)}

	svc, err := New(f.customers, f.sessions, f.hasher, 3600)
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

	return models.Credentials{Customer: models.Customer{ID: 1, Email: "a@example.com", Status: status}, PasswordHash: hash}
}

// expectSession expects one session to be created for customer 1 and returns a pointer to the hash it stored.
func (f fixture) expectSession() *string {
	stored := new(string)

	f.sessions.EXPECT().Create(f.ctx, int64(1), gomock.Any(), 3600, "test-agent", "198.51.100.7").DoAndReturn(
		func(_ *gofr.Context, _ int64, hash string, _ int, _, _ string) (time.Time, error) {
			*stored = hash

			return fixedNow.Add(time.Hour), nil
		})

	return stored
}

func TestRegister_CreatesTheCustomerAndSignsThemIn(t *testing.T) {
	f := setup(t)

	f.customers.EXPECT().Create(f.ctx, &models.RegisterRequest{Name: "Asha", Email: "asha@example.com", Phone: "+91 98765"}, gomock.Any()).
		DoAndReturn(func(_ *gofr.Context, _ *models.RegisterRequest, hash string) (models.Customer, error) {
			assert.NoError(t, f.hasher.Compare(hash, goodPassword), "a hash is stored, not the password")
			assert.NotEqual(t, goodPassword, hash)

			return models.Customer{ID: 1, Email: "asha@example.com"}, nil
		})

	stored := f.expectSession()

	got, err := f.svc.Register(f.ctx, &models.RegisterRequest{Name: " Asha ", Email: " Asha@Example.com ", Phone: "+91 98765", Password: goodPassword})

	assert.NoError(t, err)
	assert.Equal(t, HashToken(got.Token), *stored)
	assert.Equal(t, int64(1), got.Customer.ID)
}

func TestRegister_RejectsBadInputBeforeTouchingTheStore(t *testing.T) {
	f := setup(t)

	tests := map[string]models.RegisterRequest{
		"email":    {Name: "A", Email: "nope", Password: goodPassword},
		"name":     {Name: " ", Email: "a@example.com", Password: goodPassword},
		"phone":    {Name: "A", Email: "a@example.com", Phone: "call me", Password: goodPassword},
		"password": {Name: "A", Email: "a@example.com", Password: "short"},
	}

	for field, req := range tests {
		_, err := f.svc.Register(f.ctx, &req)
		assert.Equal(t, gofrhttp.ErrorInvalidParam{Params: []string{field}}, err, field)
	}
}

func TestRegister_DuplicateEmailIsAConflict(t *testing.T) {
	f := setup(t)

	f.customers.EXPECT().Create(f.ctx, gomock.Any(), gomock.Any()).Return(models.Customer{}, apperr.ErrConflict)

	_, err := f.svc.Register(f.ctx, &models.RegisterRequest{Name: "A", Email: "a@example.com", Password: goodPassword})

	assert.Equal(t, gofrhttp.ErrorEntityAlreadyExist{}, err)
}

func TestLogin_Success(t *testing.T) {
	f := setup(t)

	f.customers.EXPECT().Credentials(f.ctx, "a@example.com").Return(f.creds(t, models.StatusActive), nil)
	f.customers.EXPECT().RecordSuccess(f.ctx, int64(1)).Return(nil)

	stored := f.expectSession()

	got, err := f.svc.Login(f.ctx, &models.LoginRequest{Email: "  A@Example.COM ", Password: goodPassword})

	assert.NoError(t, err)
	assert.Len(t, got.Token, 43, "32 random bytes, base64url without padding")
	assert.Equal(t, HashToken(got.Token), *stored, "the store receives the hash")
	assert.NotEqual(t, got.Token, *stored, "the token itself is never stored")
}

func TestLogin_UnknownAndMalformedLoginsLookLikeAWrongPassword(t *testing.T) {
	f := setup(t)

	f.customers.EXPECT().Credentials(f.ctx, "nobody@example.com").Return(models.Credentials{}, apperr.ErrNotFound)

	var unauthorized *apperr.Unauthorized

	_, err := f.svc.Login(f.ctx, &models.LoginRequest{Email: "nobody@example.com", Password: goodPassword})
	assert.ErrorAs(t, err, &unauthorized)
	assert.Equal(t, invalidCreds, unauthorized.Reason)

	_, err = f.svc.Login(f.ctx, &models.LoginRequest{Email: "not-an-email", Password: goodPassword})
	assert.ErrorAs(t, err, &unauthorized, "no store call for a malformed email")

	_, err = f.svc.Login(f.ctx, &models.LoginRequest{Email: "a@example.com", Password: ""})
	assert.ErrorAs(t, err, &unauthorized, "no store call for an empty password")
}

func TestLogin_WrongPasswordCountsAndLocksAtTheLimit(t *testing.T) {
	f := setup(t)

	f.customers.EXPECT().Credentials(f.ctx, "a@example.com").Return(f.creds(t, models.StatusActive), nil).Times(2)
	f.customers.EXPECT().RecordFailure(f.ctx, int64(1), maxFailures, lockSeconds).Return(false, nil)
	f.customers.EXPECT().RecordFailure(f.ctx, int64(1), maxFailures, lockSeconds).Return(true, nil)

	var (
		unauthorized *apperr.Unauthorized
		locked       *apperr.Locked
	)

	_, err := f.svc.Login(f.ctx, &models.LoginRequest{Email: "a@example.com", Password: "wrong-password"})
	assert.ErrorAs(t, err, &unauthorized)

	_, err = f.svc.Login(f.ctx, &models.LoginRequest{Email: "a@example.com", Password: "wrong-password"})
	assert.ErrorAs(t, err, &locked, "the attempt that hits the limit is reported as 429")
}

func TestLogin_ALockedAccountIsRefusedEvenWithTheRightPassword(t *testing.T) {
	f := setup(t)

	creds := f.creds(t, models.StatusActive)
	until := fixedNow.Add(time.Minute)
	creds.LockedUntil = &until

	f.customers.EXPECT().Credentials(f.ctx, "a@example.com").Return(creds, nil)

	var locked *apperr.Locked

	_, err := f.svc.Login(f.ctx, &models.LoginRequest{Email: "a@example.com", Password: goodPassword})
	assert.ErrorAs(t, err, &locked)
}

func TestLogin_AnExpiredLockIsClearedAndLoginProceeds(t *testing.T) {
	f := setup(t)

	creds := f.creds(t, models.StatusActive)
	until := fixedNow.Add(-time.Minute)
	creds.LockedUntil = &until

	f.customers.EXPECT().Credentials(f.ctx, "a@example.com").Return(creds, nil)
	f.customers.EXPECT().ClearExpiredLock(f.ctx, int64(1)).Return(nil)
	f.customers.EXPECT().RecordSuccess(f.ctx, int64(1)).Return(nil)
	f.expectSession()

	_, err := f.svc.Login(f.ctx, &models.LoginRequest{Email: "a@example.com", Password: goodPassword})
	assert.NoError(t, err)
}

func TestLogin_ASuspendedCustomerIsForbiddenOnlyAfterTheRightPassword(t *testing.T) {
	f := setup(t)

	f.customers.EXPECT().Credentials(f.ctx, "a@example.com").Return(f.creds(t, models.StatusSuspended), nil).Times(2)
	f.customers.EXPECT().RecordFailure(f.ctx, int64(1), maxFailures, lockSeconds).Return(false, nil)

	var (
		forbidden    *apperr.Forbidden
		unauthorized *apperr.Unauthorized
	)

	_, err := f.svc.Login(f.ctx, &models.LoginRequest{Email: "a@example.com", Password: goodPassword})
	assert.ErrorAs(t, err, &forbidden)

	_, err = f.svc.Login(f.ctx, &models.LoginRequest{Email: "a@example.com", Password: "wrong-password"})
	assert.ErrorAs(t, err, &unauthorized, "a wrong password cannot probe whether an account is suspended")
}

func TestAuthenticate(t *testing.T) {
	f := setup(t)

	live := models.SessionLookup{ExpiresAt: fixedNow.Add(time.Hour), Customer: models.Customer{ID: 7, Status: models.StatusActive}}
	f.sessions.EXPECT().Lookup(f.ctx, HashToken("good")).Return(live, nil)

	p, err := f.svc.Authenticate(f.ctx, " good ")
	assert.NoError(t, err)
	assert.Equal(t, int64(7), p.Customer.ID)

	var (
		unauthorized *apperr.Unauthorized
		forbidden    *apperr.Forbidden
	)

	_, err = f.svc.Authenticate(f.ctx, "")
	assert.ErrorAs(t, err, &unauthorized, "a missing token never reaches the store")

	f.sessions.EXPECT().Lookup(f.ctx, HashToken("stale")).Return(models.SessionLookup{}, apperr.ErrNotFound)

	_, err = f.svc.Authenticate(f.ctx, "stale")
	assert.ErrorAs(t, err, &unauthorized)

	live.Customer.Status = models.StatusSuspended
	f.sessions.EXPECT().Lookup(f.ctx, HashToken("susp")).Return(live, nil)

	_, err = f.svc.Authenticate(f.ctx, "susp")
	assert.ErrorAs(t, err, &forbidden, "a suspended customer is out even with a live session")
}

func TestLogout(t *testing.T) {
	f := setup(t)

	f.sessions.EXPECT().Revoke(f.ctx, HashToken("tok")).Return(nil)
	assert.NoError(t, f.svc.Logout(f.ctx, "tok"))

	var unauthorized *apperr.Unauthorized

	assert.ErrorAs(t, f.svc.Logout(f.ctx, " "), &unauthorized)
}
