// Package user holds the business rules for users.
package user

import (
	"errors"
	"net/mail"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/account-svc/common/httpx"
	"github.com/zopping/account-svc/common/password"
	"github.com/zopping/account-svc/filters"
	"github.com/zopping/account-svc/models"
	"github.com/zopping/account-svc/services"
	"github.com/zopping/account-svc/stores"
)

const maxEmailLength = 254

var _ services.User = (*Service)(nil)

// Service implements services.User.
type Service struct {
	users    stores.User
	sessions stores.Session
	hasher   *password.Hasher
}

// New returns a user service.
func New(users stores.User, sessions stores.Session, hasher *password.Hasher) *Service {
	return &Service{users: users, sessions: sessions, hasher: hasher}
}

// NormaliseEmail trims and lower-cases an address and reports whether it is a plain, valid address.
func NormaliseEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > maxEmailLength {
		return "", false
	}

	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || !strings.Contains(email[strings.LastIndex(email, "@"):], ".") {
		return "", false
	}

	return email, true
}

// roleSet validates role ids and removes duplicates, keeping order.
func roleSet(ids []int64) ([]int64, error) {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))

	for _, id := range ids {
		if id < 1 {
			return nil, httpx.InvalidParam("roleIds")
		}

		if !seen[id] {
			seen[id] = true

			out = append(out, id)
		}
	}

	return out, nil
}

// normalise validates the fields shared by create and update.
func normalise(req *models.UserRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return httpx.InvalidParam("name")
	}

	req.Name, req.Phone = strings.TrimSpace(req.Name), strings.TrimSpace(req.Phone)

	email, ok := NormaliseEmail(req.Email)
	if !ok {
		return httpx.InvalidParam("email")
	}

	req.Email = email

	if req.Status == "" {
		req.Status = models.UserStatusActive
	}

	if !models.ValidUserStatus(req.Status) {
		return httpx.InvalidParam("status")
	}

	ids, err := roleSet(req.RoleIDs)
	if err != nil {
		return err
	}

	req.RoleIDs = ids

	return nil
}

// Create validates and creates a user; the password is hashed here and never stored in clear.
func (s *Service) Create(ctx *gofr.Context, req *models.UserRequest) (models.User, error) {
	if err := normalise(req); err != nil {
		return models.User{}, err
	}

	if !password.Valid(req.Password) {
		return models.User{}, httpx.InvalidParam("password")
	}

	hash, err := s.hasher.Hash(req.Password)
	if err != nil {
		return models.User{}, err
	}

	u, err := s.users.Create(ctx, req, hash)
	if err != nil {
		return models.User{}, httpx.FromStore(err, "user", req.Email)
	}

	return u, nil
}

// Get returns one user.
func (s *Service) Get(ctx *gofr.Context, id int64) (models.User, error) {
	u, err := s.users.Get(ctx, id)
	if err != nil {
		return models.User{}, httpx.FromStoreID(err, "id", id)
	}

	return u, nil
}

// List returns one page of users.
func (s *Service) List(ctx *gofr.Context, f *filters.UserFilter) (models.UserList, error) {
	if f.Status != "" && !models.ValidUserStatus(f.Status) {
		return models.UserList{}, httpx.InvalidParam("status")
	}

	users, count, err := s.users.List(ctx, f)
	if err != nil {
		return models.UserList{}, httpx.FromStore(err, "filter", "")
	}

	return models.UserList{Users: users, Count: count, Offset: f.Offset, Limit: f.Limit}, nil
}

// Update validates and replaces a user's profile, status, attributes and roles. The password is not changed here.
func (s *Service) Update(ctx *gofr.Context, id int64, req *models.UserRequest) (models.User, error) {
	if err := normalise(req); err != nil {
		return models.User{}, err
	}

	u, err := s.users.Update(ctx, id, req)
	if err != nil {
		return models.User{}, httpx.FromStoreID(err, "id", id)
	}

	return u, nil
}

// Delete soft-deletes a user and revokes their sessions.
func (s *Service) Delete(ctx *gofr.Context, id int64) error {
	if err := s.users.Delete(ctx, id); err != nil {
		return httpx.FromStoreID(err, "id", id)
	}

	return nil
}

// ChangePassword checks the current password, then sets the new one and revokes every session.
func (s *Service) ChangePassword(ctx *gofr.Context, id int64, req *models.PasswordChangeRequest) error {
	if !password.Valid(req.NewPassword) {
		return httpx.InvalidParam("newPassword")
	}

	u, err := s.users.Get(ctx, id)
	if err != nil {
		return httpx.FromStoreID(err, "id", id)
	}

	creds, err := s.users.Credentials(ctx, u.Email)
	if err != nil {
		return httpx.FromStoreID(err, "id", id)
	}

	if err := s.hasher.Compare(creds.PasswordHash, req.CurrentPassword); err != nil {
		if errors.Is(err, password.ErrMismatch) {
			return httpx.InvalidParam("currentPassword")
		}

		return err
	}

	return s.setPassword(ctx, id, req.NewPassword)
}

// ResetPassword sets a new password without the old one and revokes every session.
func (s *Service) ResetPassword(ctx *gofr.Context, id int64, req *models.PasswordResetRequest) error {
	if !password.Valid(req.NewPassword) {
		return httpx.InvalidParam("newPassword")
	}

	return s.setPassword(ctx, id, req.NewPassword)
}

func (s *Service) setPassword(ctx *gofr.Context, id int64, newPassword string) error {
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return err
	}

	if err := s.users.SetPassword(ctx, id, hash); err != nil {
		return httpx.FromStoreID(err, "id", id)
	}

	return nil
}

// RevokeSessions signs a user out everywhere.
func (s *Service) RevokeSessions(ctx *gofr.Context, id int64) error {
	if _, err := s.users.Get(ctx, id); err != nil {
		return httpx.FromStoreID(err, "id", id)
	}

	if err := s.sessions.RevokeUser(ctx, id); err != nil {
		return httpx.FromStoreID(err, "id", id)
	}

	return nil
}
