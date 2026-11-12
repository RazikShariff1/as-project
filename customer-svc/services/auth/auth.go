// Package auth holds registration, login and token authentication for customers.
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

	"github.com/zopping/customer-svc/clients/communication"
	"github.com/zopping/customer-svc/common/apperr"
	"github.com/zopping/customer-svc/common/authctx"
	"github.com/zopping/customer-svc/common/httpx"
	"github.com/zopping/customer-svc/common/password"
	"github.com/zopping/customer-svc/models"
	"github.com/zopping/customer-svc/services"
	customerservice "github.com/zopping/customer-svc/services/customer"
	"github.com/zopping/customer-svc/stores"
)

const (
	tokenBytes    = 32
	maxFailures   = 5
	lockSeconds   = 15 * 60
	invalidCreds  = "invalid credentials"
	suspendedMsg  = "account is suspended"
	lockedMsg     = "too many failed login attempts; try again later"
	dummyPassword = "not-a-real-password"
)

var _ services.Auth = (*Service)(nil)

// Service implements services.Auth.
type Service struct {
	customers  stores.Customer
	sessions   stores.Session
	hasher     *password.Hasher
	ttlSeconds int
	now        func() time.Time
	dummyHash  string
	notifier   communication.Notifier
}

// New returns an auth service. Sessions live ttlSeconds. It hashes one dummy password up front so a login for an
// unknown email costs the same as a wrong password and cannot be told apart by timing.
func New(customers stores.Customer, sessions stores.Session, hasher *password.Hasher, ttlSeconds int) (*Service, error) {
	dummy, err := hasher.Hash(dummyPassword)
	if err != nil {
		return nil, err
	}

	return &Service{customers: customers, sessions: sessions, hasher: hasher, ttlSeconds: ttlSeconds, now: time.Now, dummyHash: dummy, notifier: communication.Nop{}}, nil
}

// WithNotifier sets who sends the shopper emails (the welcome after registering). Without it none are sent.
func (s *Service) WithNotifier(n communication.Notifier) *Service {
	s.notifier = n

	return s
}

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

// Register creates a customer and signs them in.
func (s *Service) Register(ctx *gofr.Context, req *models.RegisterRequest) (models.Session, error) {
	email, ok := customerservice.NormaliseEmail(req.Email)
	if !ok {
		return models.Session{}, httpx.InvalidParam("email")
	}

	name, phone, err := customerservice.NormaliseProfile(req.Name, req.Phone)
	if err != nil {
		return models.Session{}, err
	}

	if !password.Valid(req.Password) {
		return models.Session{}, httpx.InvalidParam("password")
	}

	hash, err := s.hasher.Hash(req.Password)
	if err != nil {
		return models.Session{}, err
	}

	c, err := s.customers.Create(ctx, &models.RegisterRequest{Name: name, Email: email, Phone: phone}, hash)
	if err != nil {
		return models.Session{}, httpx.FromStore(err, "email", email)
	}

	session, err := s.open(ctx, c)
	if err != nil {
		return models.Session{}, err
	}

	s.notifier.Welcome(ctx, c.Name, c.Email)

	return session, nil
}

// Login exchanges credentials for a session token.
func (s *Service) Login(ctx *gofr.Context, req *models.LoginRequest) (models.Session, error) {
	email, ok := customerservice.NormaliseEmail(req.Email)
	if !ok || req.Password == "" {
		s.burn(req.Password)

		return models.Session{}, &apperr.Unauthorized{Reason: invalidCreds}
	}

	creds, err := s.customers.Credentials(ctx, email)
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
		return models.Session{}, s.failed(ctx, creds.Customer.ID, err)
	}

	if creds.Customer.Status != models.StatusActive {
		return models.Session{}, &apperr.Forbidden{Reason: suspendedMsg}
	}

	if err := s.customers.RecordSuccess(ctx, creds.Customer.ID); err != nil {
		return models.Session{}, httpx.FromStore(err, "id", "")
	}

	return s.open(ctx, creds.Customer)
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

	return httpx.FromStore(s.customers.ClearExpiredLock(ctx, creds.Customer.ID), "email", "")
}

// failed counts a wrong password and reports 429 once the account locks, else 401. Any other error is returned as is.
func (s *Service) failed(ctx *gofr.Context, id int64, cause error) error {
	if !errors.Is(cause, password.ErrMismatch) {
		return cause
	}

	locked, err := s.customers.RecordFailure(ctx, id, maxFailures, lockSeconds)
	if err != nil {
		return httpx.FromStore(err, "email", "")
	}

	if locked {
		return &apperr.Locked{Reason: lockedMsg}
	}

	return &apperr.Unauthorized{Reason: invalidCreds}
}

// open creates the session for a customer.
func (s *Service) open(ctx *gofr.Context, c models.Customer) (models.Session, error) {
	token, err := newToken()
	if err != nil {
		return models.Session{}, err
	}

	expires, err := s.sessions.Create(ctx, c.ID, HashToken(token), s.ttlSeconds, authctx.UserAgent(ctx), authctx.IP(ctx))
	if err != nil {
		return models.Session{}, httpx.FromStore(err, "id", "")
	}

	return models.Session{Token: token, ExpiresAt: expires, Customer: c}, nil
}

// Authenticate identifies the owner of a token. A suspended customer is refused even if the session is still live.
func (s *Service) Authenticate(ctx *gofr.Context, token string) (models.Principal, error) {
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

	if l.Customer.Status != models.StatusActive {
		return models.Principal{}, &apperr.Forbidden{Reason: suspendedMsg}
	}

	return models.Principal{Customer: l.Customer, ExpiresAt: l.ExpiresAt}, nil
}

// Logout revokes the token's session; repeating it is safe.
func (s *Service) Logout(ctx *gofr.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return &apperr.Unauthorized{Reason: "missing token"}
	}

	return httpx.FromStore(s.sessions.Revoke(ctx, HashToken(token)), "token", "")
}
