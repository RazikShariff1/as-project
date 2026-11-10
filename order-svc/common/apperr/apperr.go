// Package apperr holds the sentinel errors shared by the store and service layers.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

var (
	// ErrNotFound means the requested row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict means a uniqueness constraint was violated or the request clashes with current state.
	ErrConflict = errors.New("already exists")
	// ErrDuplicateClient means an order with the same customer and client id already exists: the request is a retry.
	ErrDuplicateClient = errors.New("an order with this clientId already exists")
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

// InsufficientStock means one seller's offer for a variant cannot cover the requested quantity.
type InsufficientStock struct {
	VariantID int64
	SellerID  int64
	Requested int64
	Available int64
}

func (e *InsufficientStock) Error() string {
	return fmt.Sprintf("insufficient stock for variant %d from seller %d: requested %d, available %d",
		e.VariantID, e.SellerID, e.Requested, e.Available)
}

// StateConflict means the request is valid but clashes with the current state (e.g. confirming a released reservation).
type StateConflict struct {
	Reason string
}

func (e *StateConflict) Error() string { return e.Reason }

// Forbidden means the caller is known but not allowed to do this (for example a seller touching another seller's stock). It
// carries its own status so GoFr answers 403 without a mapping step.
type Forbidden struct{ Reason string }

func (e *Forbidden) Error() string { return e.Reason }

// StatusCode makes GoFr answer 403.
func (*Forbidden) StatusCode() int { return http.StatusForbidden }
