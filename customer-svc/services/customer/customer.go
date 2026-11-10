// Package customer holds the business rules for customer profiles, passwords and staff management.
package customer

import (
	"errors"
	"net/mail"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/customer-svc/common/apperr"
	"github.com/zopping/customer-svc/common/httpx"
	"github.com/zopping/customer-svc/common/password"
	"github.com/zopping/customer-svc/filters"
	"github.com/zopping/customer-svc/models"
	"github.com/zopping/customer-svc/services"
	"github.com/zopping/customer-svc/stores"
)

const (
	maxEmailLength = 254
	maxNameLength  = 100
	maxPhoneLength = 20
)

var _ services.Customer = (*Service)(nil)

// Service implements services.Customer.
type Service struct {
	customers stores.Customer
	hasher    *password.Hasher
}

// New returns a customer service.
func New(customers stores.Customer, hasher *password.Hasher) *Service {
	return &Service{customers: customers, hasher: hasher}
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

// NormaliseProfile trims and validates a name and an optional phone number, returning the cleaned values.
func NormaliseProfile(name, phone string) (cleanName, cleanPhone string, err error) {
	name, phone = strings.TrimSpace(name), strings.TrimSpace(phone)

	if name == "" || len(name) > maxNameLength {
		return "", "", httpx.InvalidParam("name")
	}

	if len(phone) > maxPhoneLength || strings.ContainsFunc(phone, func(r rune) bool {
		return (r < '0' || r > '9') && !strings.ContainsRune("+-() ", r)
	}) {
		return "", "", httpx.InvalidParam("phone")
	}

	return name, phone, nil
}

// Get returns one customer.
func (s *Service) Get(ctx *gofr.Context, id int64) (models.Customer, error) {
	c, err := s.customers.Get(ctx, id)

	return c, httpx.FromStoreID(err, "id", id)
}

// List returns one page of customers.
func (s *Service) List(ctx *gofr.Context, f *filters.CustomerFilter) (models.CustomerList, error) {
	if f.Status != "" && !models.ValidStatus(f.Status) {
		return models.CustomerList{}, httpx.InvalidParam("status")
	}

	customers, count, err := s.customers.List(ctx, f)
	if err != nil {
		return models.CustomerList{}, httpx.FromStore(err, "customer", "")
	}

	return models.CustomerList{Customers: customers, Count: count, Offset: f.Offset, Limit: f.Limit}, nil
}

// UpdateProfile changes a customer's name and phone.
func (s *Service) UpdateProfile(ctx *gofr.Context, id int64, req *models.ProfileRequest) (models.Customer, error) {
	name, phone, err := NormaliseProfile(req.Name, req.Phone)
	if err != nil {
		return models.Customer{}, err
	}

	c, err := s.customers.UpdateProfile(ctx, id, &models.ProfileRequest{Name: name, Phone: phone})

	return c, httpx.FromStoreID(err, "id", id)
}

// ChangePassword sets a new password after checking the current one. Every session is revoked, this one included.
func (s *Service) ChangePassword(ctx *gofr.Context, id int64, req *models.PasswordChangeRequest) error {
	if !password.Valid(req.NewPassword) {
		return httpx.InvalidParam("newPassword")
	}

	// Credentials looks a customer up by email, so fetch the email first.
	c, err := s.customers.Get(ctx, id)
	if err != nil {
		return httpx.FromStoreID(err, "id", id)
	}

	creds, err := s.customers.Credentials(ctx, c.Email)
	if err != nil {
		return httpx.FromStoreID(err, "id", id)
	}

	if err := s.hasher.Compare(creds.PasswordHash, req.CurrentPassword); err != nil {
		if errors.Is(err, password.ErrMismatch) {
			return &apperr.Unauthorized{Reason: "current password is wrong"}
		}

		return err
	}

	hash, err := s.hasher.Hash(req.NewPassword)
	if err != nil {
		return err
	}

	return httpx.FromStoreID(s.customers.SetPassword(ctx, id, hash), "id", id)
}

// SetStatus suspends or reactivates a customer. Suspending ends their sessions.
func (s *Service) SetStatus(ctx *gofr.Context, id int64, req *models.StatusRequest) (models.Customer, error) {
	if !models.ValidStatus(req.Status) {
		return models.Customer{}, httpx.InvalidParam("status")
	}

	c, err := s.customers.SetStatus(ctx, id, req.Status)

	return c, httpx.FromStoreID(err, "id", id)
}
