// Package stores declares the data-access contracts; implementations live in one sub-package per store.
// Reads never return soft-deleted rows (deleted_at set); Delete is always a soft delete.
package stores

import (
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/account-svc/filters"
	"github.com/zopping/account-svc/models"
)

// User is the data-access contract for users. Create and Update also write the user's roles (user_role) in the same
// transaction; reads fill RoleIDs.
type User interface {
	// Create inserts a user with an already-hashed password. A duplicate email yields apperr.ErrConflict; an unknown
	// or deleted role yields apperr.InvalidRef (user_role_role_id_fkey).
	Create(ctx *gofr.Context, req *models.UserRequest, passwordHash string) (models.User, error)
	Get(ctx *gofr.Context, id int64) (models.User, error)
	List(ctx *gofr.Context, f *filters.UserFilter) (users []models.User, count int, err error)
	Update(ctx *gofr.Context, id int64, req *models.UserRequest) (models.User, error)
	// Delete soft-deletes the user and revokes all of their sessions in one transaction.
	Delete(ctx *gofr.Context, id int64) error
	Count(ctx *gofr.Context) (int, error)

	// Credentials returns what login needs, looked up by lower-case email; apperr.ErrNotFound if there is no such user.
	Credentials(ctx *gofr.Context, email string) (models.Credentials, error)
	// SetPassword replaces the hash, clears any lockout and revokes every session, in one transaction.
	SetPassword(ctx *gofr.Context, id int64, passwordHash string) error
	// RecordFailure counts a failed login atomically and locks the account for lockSeconds once maxFailures is
	// reached; it reports whether the account is now locked.
	RecordFailure(ctx *gofr.Context, id int64, maxFailures, lockSeconds int) (locked bool, err error)
	RecordSuccess(ctx *gofr.Context, id int64) error
	// ClearExpiredLock resets the failure count of an account whose lock has run out.
	ClearExpiredLock(ctx *gofr.Context, id int64) error
}

// Role is the data-access contract for roles. Create and Update also write the role's endpoints (endpoint_role) in
// the same transaction; reads fill EndpointIDs. Delete removes the role's user and endpoint links.
type Role interface {
	Create(ctx *gofr.Context, req *models.RoleRequest) (models.Role, error)
	Get(ctx *gofr.Context, id int64) (models.Role, error)
	List(ctx *gofr.Context, f *filters.RoleFilter) (roles []models.Role, count int, err error)
	Update(ctx *gofr.Context, id int64, req *models.RoleRequest) (models.Role, error)
	Delete(ctx *gofr.Context, id int64) error
	// Permissions reports whether any of the user's roles has all access, and the path patterns of the endpoints
	// their roles grant for the given service and method.
	Permissions(ctx *gofr.Context, userID int64, service, method string) (allAccess bool, patterns []string, err error)
}

// Endpoint is the data-access contract for endpoints. Delete also removes the endpoint's role links.
type Endpoint interface {
	Create(ctx *gofr.Context, req *models.EndpointRequest) (models.Endpoint, error)
	Get(ctx *gofr.Context, id int64) (models.Endpoint, error)
	List(ctx *gofr.Context, f *filters.EndpointFilter) (endpoints []models.Endpoint, count int, err error)
	Update(ctx *gofr.Context, id int64, req *models.EndpointRequest) (models.Endpoint, error)
	Delete(ctx *gofr.Context, id int64) error
}

// Session is the data-access contract for login sessions. Only the SHA-256 of a token is ever stored.
type Session interface {
	// Create stores a session that expires ttlSeconds from now (database clock) and returns its expiry.
	Create(ctx *gofr.Context, userID int64, tokenHash string, ttlSeconds int, userAgent, ip string) (time.Time, error)
	// Lookup returns the user behind a live token; apperr.ErrNotFound if it is unknown, expired, revoked, or its
	// user was deleted.
	Lookup(ctx *gofr.Context, tokenHash string) (models.SessionLookup, error)
	// Revoke ends the session. It succeeds whether or not the token was live, so logout is safe to repeat.
	Revoke(ctx *gofr.Context, tokenHash string) error
	RevokeUser(ctx *gofr.Context, userID int64) error
}
