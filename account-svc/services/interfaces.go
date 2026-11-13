// Package services declares the business-logic contracts; implementations live in one sub-package per domain.
package services

import (
	"context"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/account-svc/filters"
	"github.com/zopping/account-svc/models"
)

// User is the business-logic contract for users.
type User interface {
	Create(ctx *gofr.Context, req *models.UserRequest) (models.User, error)
	Get(ctx *gofr.Context, id int64) (models.User, error)
	List(ctx *gofr.Context, f *filters.UserFilter) (models.UserList, error)
	Update(ctx *gofr.Context, id int64, req *models.UserRequest) (models.User, error)
	Delete(ctx *gofr.Context, id int64) error
	// ChangePassword verifies the current password first. Both it and ResetPassword revoke every session.
	ChangePassword(ctx *gofr.Context, id int64, req *models.PasswordChangeRequest) error
	ResetPassword(ctx *gofr.Context, id int64, req *models.PasswordResetRequest) error
	RevokeSessions(ctx *gofr.Context, id int64) error
}

// Role is the business-logic contract for roles.
type Role interface {
	Create(ctx *gofr.Context, req *models.RoleRequest) (models.Role, error)
	Get(ctx *gofr.Context, id int64) (models.Role, error)
	List(ctx *gofr.Context, f *filters.RoleFilter) (models.RoleList, error)
	Update(ctx *gofr.Context, id int64, req *models.RoleRequest) (models.Role, error)
	Delete(ctx *gofr.Context, id int64) error
}

// Endpoint is the business-logic contract for endpoints.
type Endpoint interface {
	Create(ctx *gofr.Context, req *models.EndpointRequest) (models.Endpoint, error)
	Get(ctx *gofr.Context, id int64) (models.Endpoint, error)
	List(ctx *gofr.Context, f *filters.EndpointFilter) (models.EndpointList, error)
	Update(ctx *gofr.Context, id int64, req *models.EndpointRequest) (models.Endpoint, error)
	Delete(ctx *gofr.Context, id int64) error
}

// Auth is the business-logic contract for login, tokens and permission checks.
type Auth interface {
	// Login exchanges credentials for a session token. Failures are deliberately indistinguishable (401), a suspended
	// account is 403, and repeated failures lock the account (429).
	Login(ctx *gofr.Context, req *models.LoginRequest) (models.Session, error)
	// GoogleLogin exchanges a Google ID token for a session token. It never creates an account: the token's verified
	// email must belong to an existing, active user (401 bad token, 403 no such user or suspended, 503 not enabled).
	GoogleLogin(ctx *gofr.Context, req *models.GoogleLoginRequest) (models.Session, error)
	// Authenticate identifies the owner of a token: 401 if it is missing, unknown, expired or revoked, 403 if the
	// account is suspended.
	Authenticate(ctx *gofr.Context, token string) (models.Principal, error)
	// Authorize is Authenticate plus a permission check for one service, method and path: 403 if not permitted.
	Authorize(ctx *gofr.Context, token, service, method, path string) (models.Principal, error)
	// Logout revokes the token's session; repeating it is safe.
	Logout(ctx *gofr.Context, token string) error
	// Bootstrap creates the first admin when no user exists yet and reports whether it did.
	Bootstrap(ctx *gofr.Context, email, password string) (bool, error)
}

// GoogleVerifier checks a Google ID token and says who it vouches for. A forged, expired or foreign token is an
// *apperr.Unauthorized; any other error means Google could not be reached.
type GoogleVerifier interface {
	Verify(ctx context.Context, idToken string) (models.GoogleIdentity, error)
}
