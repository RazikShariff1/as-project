// Package apperr holds the sentinel errors shared by the store and service layers.
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
	// ErrInvalid means an id was malformed or referenced a row that does not exist.
	ErrInvalid = errors.New("invalid id or reference")
)

// InvalidRef is an ErrInvalid caused by a foreign-key violation; Constraint names the failing constraint
// so the HTTP layer can tell the client which field was wrong.
type InvalidRef struct {
	Constraint string
}

func (e *InvalidRef) Error() string { return ErrInvalid.Error() + " (" + e.Constraint + ")" }

// Is makes errors.Is(err, ErrInvalid) true for an InvalidRef.
func (*InvalidRef) Is(target error) bool { return target == ErrInvalid }

// Forbidden means the caller is known but not allowed to do this (for example a seller who is not permitted to sell a
// product). It carries its own status so GoFr answers 403 without a mapping step.
type Forbidden struct{ Reason string }

func (e *Forbidden) Error() string { return e.Reason }

// StatusCode makes GoFr answer 403.
func (*Forbidden) StatusCode() int { return http.StatusForbidden }

// StateConflict means the request is valid but clashes with the current state (for example editing an approved
// request). It carries its own status so GoFr answers 409 without a mapping step.
type StateConflict struct{ Reason string }

func (e *StateConflict) Error() string { return e.Reason }

// StatusCode makes GoFr answer 409.
func (*StateConflict) StatusCode() int { return http.StatusConflict }
