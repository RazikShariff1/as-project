// Package apperr holds the sentinel and typed errors shared by the store and service layers.
package apperr

import (
	"errors"
	"net/http"
)

var (
	// ErrNotFound means the requested row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict means a uniqueness constraint was violated.
	ErrConflict = errors.New("already exists")
	// ErrInvalid means an id was malformed or a value broke a constraint.
	ErrInvalid = errors.New("invalid id or reference")
)

// InvalidRef is an ErrInvalid caused by a constraint violation; Constraint names the failing constraint
// so the HTTP layer can tell the client which field was wrong.
type InvalidRef struct {
	Constraint string
}

func (e *InvalidRef) Error() string { return ErrInvalid.Error() + " (" + e.Constraint + ")" }

// Is makes errors.Is(err, ErrInvalid) true for an InvalidRef.
func (*InvalidRef) Is(target error) bool { return target == ErrInvalid }

// Unauthorized means the caller could not be identified (bad credentials, or a missing, expired or revoked token).
type Unauthorized struct{ Reason string }

func (e *Unauthorized) Error() string { return e.Reason }

// StatusCode makes GoFr answer 401 without any mapping step.
func (*Unauthorized) StatusCode() int { return http.StatusUnauthorized }

// Forbidden means the caller is known but not allowed (suspended account, or no permission for the endpoint).
type Forbidden struct{ Reason string }

func (e *Forbidden) Error() string { return e.Reason }

// StatusCode makes GoFr answer 403 without any mapping step.
func (*Forbidden) StatusCode() int { return http.StatusForbidden }

// Locked means too many failed logins; the account accepts no login attempt until the lock expires.
type Locked struct{ Reason string }

func (e *Locked) Error() string { return e.Reason }

// StatusCode makes GoFr answer 429 without any mapping step.
func (*Locked) StatusCode() int { return http.StatusTooManyRequests }

// Unavailable means the feature is not turned on in this deployment (for example Google sign-in without a client ID).
type Unavailable struct{ Reason string }

func (e *Unavailable) Error() string { return e.Reason }

// StatusCode makes GoFr answer 503 without any mapping step.
func (*Unavailable) StatusCode() int { return http.StatusServiceUnavailable }
