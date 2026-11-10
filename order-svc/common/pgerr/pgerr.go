// Package pgerr translates Postgres driver errors into apperr sentinels.
package pgerr

import (
	"database/sql"
	"errors"

	"github.com/lib/pq"

	"github.com/zopping/order-svc/common/apperr"
)

const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
	codeCheckViolation      = "23514"
	codeInvalidText         = "22P02" // e.g. a malformed id
)

// ConstraintClientID is the unique index on (customer_id, client_id).
const ConstraintClientID = "orders_client_id_uniq"

// Map converts a database error into an apperr sentinel; unknown errors pass through unchanged.
func Map(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, sql.ErrNoRows) {
		return apperr.ErrNotFound
	}

	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch string(pqErr.Code) {
		case codeUniqueViolation:
			if pqErr.Constraint == ConstraintClientID {
				return apperr.ErrDuplicateClient
			}

			return apperr.ErrConflict
		case codeForeignKeyViolation, codeCheckViolation:
			return &apperr.InvalidRef{Constraint: pqErr.Constraint}
		case codeInvalidText:
			return apperr.ErrInvalid
		}
	}

	return err
}
