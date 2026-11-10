// Package services defines the business-logic contracts the HTTP handlers depend on.
package services

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/customer-svc/filters"
	"github.com/zopping/customer-svc/models"
)

// Auth registers customers, logs them in and identifies the owner of a token.
type Auth interface {
	Register(ctx *gofr.Context, req *models.RegisterRequest) (models.Session, error)
	Login(ctx *gofr.Context, req *models.LoginRequest) (models.Session, error)
	// Authenticate resolves a token to its live, active customer.
	Authenticate(ctx *gofr.Context, token string) (models.Principal, error)
	Logout(ctx *gofr.Context, token string) error
}

// Customer holds profile, password and staff-side customer management.
type Customer interface {
	Get(ctx *gofr.Context, id int64) (models.Customer, error)
	List(ctx *gofr.Context, f *filters.CustomerFilter) (models.CustomerList, error)
	UpdateProfile(ctx *gofr.Context, id int64, req *models.ProfileRequest) (models.Customer, error)
	ChangePassword(ctx *gofr.Context, id int64, req *models.PasswordChangeRequest) error
	SetStatus(ctx *gofr.Context, id int64, req *models.StatusRequest) (models.Customer, error)
}

// Address holds a customer's saved addresses. The customer id always comes from the caller's token, never the client.
type Address interface {
	List(ctx *gofr.Context, customerID int64) ([]models.Address, error)
	Get(ctx *gofr.Context, customerID, id int64) (models.Address, error)
	Create(ctx *gofr.Context, customerID int64, req *models.AddressRequest) (models.Address, error)
	Update(ctx *gofr.Context, customerID, id int64, req *models.AddressRequest) (models.Address, error)
	Delete(ctx *gofr.Context, customerID, id int64) error
}
