// Package auth holds login, token authentication and permission checks.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/account-svc/common/apperr"
	"github.com/zopping/account-svc/common/authctx"
	"github.com/zopping/account-svc/common/httpx"
	"github.com/zopping/account-svc/common/password"
	"github.com/zopping/account-svc/filters"
	"github.com/zopping/account-svc/models"
	"github.com/zopping/account-svc/services"
	userservice "github.com/zopping/account-svc/services/user"
	"github.com/zopping/account-svc/stores"
)

const (
	tokenBytes     = 32
	maxFailures    = 5
	lockSeconds    = 15 * 60
	adminRoleName  = "admin"
	invalidCreds   = "invalid credentials"
	suspendedMsg   = "account is suspended"
	lockedMsg      = "too many failed login attempts; try again later"
	dummyPassword  = "not-a-real-password"
	bootstrapLimit = 100

	googleOff        = "Google sign-in is not enabled"
	googleNoAccount  = "no account is linked to this Google email"
	googleUnverified = "the Google account's email is not verified"
)

var _ services.Auth = (*Service)(nil)

// Service implements services.Auth.
type Service struct {
	users      stores.User
	roles      stores.Role
	sessions   stores.Session
	hasher     *password.Hasher
	ttlSeconds int
	now        func() time.Time
	dummyHash  string
	google     services.GoogleVerifier // nil unless GOOGLE_CLIENT_ID is configured
}

// New returns an auth service. Sessions live ttlSeconds. It hashes one dummy password up front so a login for an
// unknown email costs the same as a wrong password and cannot be told apart by timing.
func New(users stores.User, roles stores.Role, sessions stores.Session, hasher *password.Hasher, ttlSeconds int) (*Service, error) {
	dummy, err := hasher.Hash(dummyPassword)
	if err != nil {
		return nil, err
	}

	return &Service{
		users: users, roles: roles, sessions: sessions, hasher: hasher,
		ttlSeconds: ttlSeconds, now: time.Now, dummyHash: dummy,
	}, nil
}

// EnableGoogle turns on Google sign-in with the verifier that checks ID tokens. Without it GoogleLogin answers 503.
func (s *Service) EnableGoogle(v services.GoogleVerifier) { s.google = v }

// HashToken returns the SHA-256 of a token as hex. Only this is ever stored.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}

func newToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Login exchanges credentials for a session token.
func (s *Service) Login(ctx *gofr.Context, req *models.LoginRequest) (models.Session, error) {
	email, ok := userservice.NormaliseEmail(req.Email)
	if !ok || req.Password == "" {
		s.burn(req.Password)

		return models.Session{}, &apperr.Unauthorized{Reason: invalidCreds}
	}

	creds, err := s.users.Credentials(ctx, email)
	if errors.Is(err, apperr.ErrNotFound) {
		s.burn(req.Password)

		return models.Session{}, &apperr.Unauthorized{Reason: invalidCreds}
	}

	if err != nil {
		return models.Session{}, httpx.FromStore(err, "email", email)
	}

	if err := s.checkLock(ctx, &creds); err != nil {
		return models.Session{}, err
	}

	if err := s.hasher.Compare(creds.PasswordHash, req.Password); err != nil {
		return models.Session{}, s.failed(ctx, creds.User.ID, err)
	}

	if creds.User.Status != models.UserStatusActive {
		return models.Session{}, &apperr.Forbidden{Reason: suspendedMsg}
	}

	return s.open(ctx, creds.User)
}

// GoogleLogin exchanges a Google ID token for a session token. Google proves the caller owns a verified email; we then
// require that email to belong to an existing, active user. It never creates an account (this is an admin panel: a
// Google account that merely exists must not become a user), and it does not touch the password lockout, which exists to
// stop password guessing and cannot be reached with a token only Google can sign.
func (s *Service) GoogleLogin(ctx *gofr.Context, req *models.GoogleLoginRequest) (models.Session, error) {
	if s.google == nil {
		return models.Session{}, &apperr.Unavailable{Reason: googleOff}
	}

	token := strings.TrimSpace(req.IDToken)
	if token == "" {
		return models.Session{}, httpx.InvalidParam("idToken")
	}

	identity, err := s.google.Verify(ctx, token)
	if err != nil {
		return models.Session{}, err
	}

	if !identity.EmailVerified {
		return models.Session{}, &apperr.Unauthorized{Reason: googleUnverified}
	}

	email, ok := userservice.NormaliseEmail(identity.Email)
	if !ok {
		return models.Session{}, &apperr.Unauthorized{Reason: googleUnverified}
	}

	creds, err := s.users.Credentials(ctx, email)
	if errors.Is(err, apperr.ErrNotFound) {
		return models.Session{}, &apperr.Forbidden{Reason: googleNoAccount}
	}

	if err != nil {
		return models.Session{}, httpx.FromStore(err, "email", email)
	}

	if creds.User.Status != models.UserStatusActive {
		return models.Session{}, &apperr.Forbidden{Reason: suspendedMsg}
	}

	return s.open(ctx, creds.User)
}

// burn spends the time of one password check so unknown and known emails look alike.
func (s *Service) burn(pw string) { _ = s.hasher.Compare(s.dummyHash, pw) }

// checkLock refuses a locked account and clears a lock that has run out.
func (s *Service) checkLock(ctx *gofr.Context, creds *models.Credentials) error {
	if creds.LockedUntil == nil {
		return nil
	}

	if s.now().Before(*creds.LockedUntil) {
		return &apperr.Locked{Reason: lockedMsg}
	}

	return httpx.FromStore(s.users.ClearExpiredLock(ctx, creds.User.ID), "email", "")
}

// failed counts a wrong password and reports 429 once the account locks, else 401. Any other error is returned as is.
func (s *Service) failed(ctx *gofr.Context, userID int64, cause error) error {
	if !errors.Is(cause, password.ErrMismatch) {
		return cause
	}

	locked, err := s.users.RecordFailure(ctx, userID, maxFailures, lockSeconds)
	if err != nil {
		return httpx.FromStore(err, "email", "")
	}

	if locked {
		return &apperr.Locked{Reason: lockedMsg}
	}

	return &apperr.Unauthorized{Reason: invalidCreds}
}

// open records the login and creates the session.
func (s *Service) open(ctx *gofr.Context, u models.User) (models.Session, error) {
	token, err := newToken()
	if err != nil {
		return models.Session{}, err
	}

	if err := s.users.RecordSuccess(ctx, u.ID); err != nil {
		return models.Session{}, httpx.FromStore(err, "id", "")
	}

	expires, err := s.sessions.Create(ctx, u.ID, HashToken(token), s.ttlSeconds, authctx.UserAgent(ctx), authctx.IP(ctx))
	if err != nil {
		return models.Session{}, httpx.FromStore(err, "id", "")
	}

	return models.Session{Token: token, ExpiresAt: expires, User: u}, nil
}

// Authenticate identifies the owner of a token and says whether their roles grant all access.
func (s *Service) Authenticate(ctx *gofr.Context, token string) (models.Principal, error) {
	p, err := s.identify(ctx, token)
	if err != nil {
		return models.Principal{}, err
	}

	allAccess, _, err := s.roles.Permissions(ctx, p.User.ID, "", "")
	if err != nil {
		return models.Principal{}, httpx.FromStore(err, "permissions", "")
	}

	p.AllAccess = allAccess

	return p, nil
}

// identify resolves a token to its live, active user without looking at permissions.
func (s *Service) identify(ctx *gofr.Context, token string) (models.Principal, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return models.Principal{}, &apperr.Unauthorized{Reason: "missing token"}
	}

	l, err := s.sessions.Lookup(ctx, HashToken(token))
	if errors.Is(err, apperr.ErrNotFound) {
		return models.Principal{}, &apperr.Unauthorized{Reason: "invalid or expired token"}
	}

	if err != nil {
		return models.Principal{}, httpx.FromStore(err, "token", "")
	}

	if l.User.Status != models.UserStatusActive {
		return models.Principal{}, &apperr.Forbidden{Reason: suspendedMsg}
	}

	return models.Principal{User: l.User, ExpiresAt: l.ExpiresAt}, nil
}

// Authorize authenticates a token and checks the user may call a method on a path of a service.
func (s *Service) Authorize(ctx *gofr.Context, token, service, method, path string) (models.Principal, error) {
	service, method = strings.TrimSpace(service), strings.ToUpper(strings.TrimSpace(method))

	switch {
	case service == "":
		return models.Principal{}, httpx.InvalidParam("service")
	case !models.ValidMethod(method):
		return models.Principal{}, httpx.InvalidParam("method")
	}

	path, ok := requestPath(path)
	if !ok {
		return models.Principal{}, httpx.InvalidParam("path")
	}

	p, err := s.identify(ctx, token)
	if err != nil {
		return models.Principal{}, err
	}

	allAccess, patterns, err := s.roles.Permissions(ctx, p.User.ID, service, method)
	if err != nil {
		return models.Principal{}, httpx.FromStore(err, "permissions", "")
	}

	p.AllAccess = allAccess
	if allAccess || anyMatch(patterns, path) {
		return p, nil
	}

	return models.Principal{}, &apperr.Forbidden{Reason: "not permitted to call " + method + " " + path + " on " + service}
}

// requestPath drops any query string and trailing slash from a request path; it must start with "/".
func requestPath(raw string) (string, bool) {
	p := strings.TrimSpace(raw)
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}

	if !strings.HasPrefix(p, "/") {
		return "", false
	}

	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}

	return p, true
}

func anyMatch(patterns []string, path string) bool {
	for _, p := range patterns {
		if Match(p, path) {
			return true
		}
	}

	return false
}

// Match reports whether a request path fits an endpoint pattern: same number of segments, each equal, except that a
// {name} segment in the pattern matches any one non-empty segment.
func Match(pattern, path string) bool {
	ps, rs := strings.Split(strings.TrimPrefix(pattern, "/"), "/"), strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(ps) != len(rs) {
		return false
	}

	for i, seg := range ps {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			if rs[i] == "" {
				return false
			}

			continue
		}

		if seg != rs[i] {
			return false
		}
	}

	return true
}

// Logout revokes the token's session; repeating it is safe.
func (s *Service) Logout(ctx *gofr.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return &apperr.Unauthorized{Reason: "missing token"}
	}

	return httpx.FromStore(s.sessions.Revoke(ctx, HashToken(token)), "token", "")
}

// Bootstrap creates the admin role and first user when no user exists. It is a no-op once any user exists, so a
// restart or a second instance cannot create another admin.
func (s *Service) Bootstrap(ctx *gofr.Context, email, pw string) (bool, error) {
	n, err := s.users.Count(ctx)
	if err != nil {
		return false, err
	}

	if n > 0 {
		return false, nil
	}

	email, ok := userservice.NormaliseEmail(email)
	if !ok || !password.Valid(pw) {
		return false, errors.New("bootstrap admin needs a valid ADMIN_EMAIL and an ADMIN_PASSWORD of 8 to 72 characters")
	}

	role, err := s.adminRole(ctx)
	if err != nil {
		return false, err
	}

	hash, err := s.hasher.Hash(pw)
	if err != nil {
		return false, err
	}

	req := &models.UserRequest{Name: "Administrator", Email: email, Status: models.UserStatusActive, RoleIDs: []int64{role.ID}}
	if _, err := s.users.Create(ctx, req, hash); err != nil {
		return false, err
	}

	return true, nil
}

// adminRole returns the all-access admin role, creating it if it does not exist yet.
func (s *Service) adminRole(ctx *gofr.Context) (models.Role, error) {
	roles, _, err := s.roles.List(ctx, &filters.RoleFilter{Name: adminRoleName, Limit: bootstrapLimit})
	if err != nil {
		return models.Role{}, err
	}

	for _, r := range roles {
		if r.Name == adminRoleName && r.AllAccess {
			return r, nil
		}
	}

	return s.roles.Create(ctx, &models.RoleRequest{Name: adminRoleName, Description: "Full access", AllAccess: true})
}
