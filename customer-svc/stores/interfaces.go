// Package stores defines the persistence contracts the services depend on.
package stores

import (
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/customer-svc/filters"
	"github.com/zopping/customer-svc/models"
)

// Customer persists customers and their login counters.
type Customer interface {
	Create(ctx *gofr.Context, req *models.RegisterRequest, passwordHash string) (models.Customer, error)
	Get(ctx *gofr.Context, id int64) (models.Customer, error)
	List(ctx *gofr.Context, f *filters.CustomerFilter) ([]models.Customer, int, error)
	UpdateProfile(ctx *gofr.Context, id int64, req *models.ProfileRequest) (models.Customer, error)
	// SetStatus changes the status; suspending also revokes every session, in one transaction.
	SetStatus(ctx *gofr.Context, id int64, status string) (models.Customer, error)
	Credentials(ctx *gofr.Context, email string) (models.Credentials, error)
	// SetPassword replaces the hash, clears any lockout and revokes every session, in one transaction.
	SetPassword(ctx *gofr.Context, id int64, passwordHash string) error
	RecordFailure(ctx *gofr.Context, id int64, maxFailures, lockSeconds int) (locked bool, err error)
	RecordSuccess(ctx *gofr.Context, id int64) error
	ClearExpiredLock(ctx *gofr.Context, id int64) error
}

// Session persists login sessions; tokens are only ever stored hashed.
type Session interface {
	Create(ctx *gofr.Context, customerID int64, tokenHash string, ttlSeconds int, userAgent, ip string) (time.Time, error)
	Lookup(ctx *gofr.Context, tokenHash string) (models.SessionLookup, error)
	Revoke(ctx *gofr.Context, tokenHash string) error
}

// Address persists a customer's saved addresses. Every call names the customer, so one can never reach another's.
type Address interface {
	List(ctx *gofr.Context, customerID int64) ([]models.Address, error)
	Get(ctx *gofr.Context, customerID, id int64) (models.Address, error)
	Create(ctx *gofr.Context, customerID int64, req *models.AddressRequest) (models.Address, error)
	Update(ctx *gofr.Context, customerID, id int64, req *models.AddressRequest) (models.Address, error)
	Delete(ctx *gofr.Context, customerID, id int64) error
}
