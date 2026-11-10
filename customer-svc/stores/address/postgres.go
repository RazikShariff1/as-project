// Package address is the Postgres store for a customer's saved addresses.
package address

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/customer-svc/common/apperr"
	"github.com/zopping/customer-svc/common/pgerr"
	"github.com/zopping/customer-svc/models"
	"github.com/zopping/customer-svc/stores"
)

const columns = `id, customer_id, label, recipient, phone, line1, line2, city, state, postal_code, country, is_default, created_at, updated_at`

var _ stores.Address = (*Store)(nil)

// Store implements stores.Address on top of ctx.SQL. Every query filters on customer_id.
type Store struct{}

// New returns an address store.
func New() *Store { return &Store{} }

func scan(row interface{ Scan(...any) error }) (a models.Address, err error) {
	err = row.Scan(&a.ID, &a.CustomerID, &a.Label, &a.Recipient, &a.Phone, &a.Line1, &a.Line2, &a.City, &a.State,
		&a.PostalCode, &a.Country, &a.IsDefault, &a.CreatedAt, &a.UpdatedAt)

	return a, err
}

// List returns a customer's addresses, default first.
func (*Store) List(ctx *gofr.Context, customerID int64) ([]models.Address, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT `+columns+` FROM addresses WHERE customer_id = $1 AND deleted_at IS NULL ORDER BY is_default DESC, id`, customerID)
	if err != nil {
		return nil, pgerr.Map(err)
	}
	defer rows.Close()

	out := []models.Address{}

	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, a)
	}

	return out, rows.Err()
}

// Get fetches one of a customer's addresses; another customer's address is not found.
func (*Store) Get(ctx *gofr.Context, customerID, id int64) (models.Address, error) {
	a, err := scan(ctx.SQL.QueryRowContext(ctx,
		`SELECT `+columns+` FROM addresses WHERE id = $1 AND customer_id = $2 AND deleted_at IS NULL`, id, customerID))

	return a, pgerr.Map(err)
}

// Create saves an address. The customer's first address becomes the default, and a new default demotes the old one in
// the same transaction (the partial unique index would refuse two).
func (*Store) Create(ctx *gofr.Context, customerID int64, req *models.AddressRequest) (a models.Address, err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return a, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Serialise a customer's address writes so two concurrent defaults cannot race.
	if _, err = tx.ExecContext(ctx, `SELECT 1 FROM customers WHERE id = $1 FOR UPDATE`, customerID); err != nil {
		return a, pgerr.Map(err)
	}

	var existing int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM addresses WHERE customer_id = $1 AND deleted_at IS NULL`, customerID).Scan(&existing); err != nil {
		return a, pgerr.Map(err)
	}

	isDefault := req.IsDefault || existing == 0
	if isDefault {
		if _, err = tx.ExecContext(ctx, `UPDATE addresses SET is_default = FALSE WHERE customer_id = $1 AND is_default`, customerID); err != nil {
			return a, pgerr.Map(err)
		}
	}

	a, err = scan(tx.QueryRowContext(ctx,
		`INSERT INTO addresses (customer_id, label, recipient, phone, line1, line2, city, state, postal_code, country, is_default)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING `+columns,
		customerID, req.Label, req.Recipient, req.Phone, req.Line1, req.Line2, req.City, req.State, req.PostalCode, req.Country, isDefault))
	if err != nil {
		return a, pgerr.Map(err)
	}

	return a, tx.Commit()
}

// Update replaces one of a customer's addresses. Setting it as default demotes the old default; an address that is
// already the default stays so (the default is changed by making another one default).
func (*Store) Update(ctx *gofr.Context, customerID, id int64, req *models.AddressRequest) (a models.Address, err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return a, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `SELECT 1 FROM customers WHERE id = $1 FOR UPDATE`, customerID); err != nil {
		return a, pgerr.Map(err)
	}

	if req.IsDefault {
		if _, err = tx.ExecContext(ctx, `UPDATE addresses SET is_default = FALSE WHERE customer_id = $1 AND is_default AND id <> $2`, customerID, id); err != nil {
			return a, pgerr.Map(err)
		}
	}

	a, err = scan(tx.QueryRowContext(ctx,
		`UPDATE addresses
		    SET label = $3, recipient = $4, phone = $5, line1 = $6, line2 = $7, city = $8, state = $9, postal_code = $10,
		        country = $11, is_default = (is_default OR $12), updated_at = now()
		  WHERE id = $1 AND customer_id = $2 AND deleted_at IS NULL RETURNING `+columns,
		id, customerID, req.Label, req.Recipient, req.Phone, req.Line1, req.Line2, req.City, req.State, req.PostalCode, req.Country, req.IsDefault))
	if err != nil {
		return a, pgerr.Map(err)
	}

	return a, tx.Commit()
}

// Delete soft-deletes an address. If it was the default, the oldest remaining address becomes the default.
func (*Store) Delete(ctx *gofr.Context, customerID, id int64) (err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `SELECT 1 FROM customers WHERE id = $1 FOR UPDATE`, customerID); err != nil {
		return pgerr.Map(err)
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE addresses SET deleted_at = now(), is_default = FALSE WHERE id = $1 AND customer_id = $2 AND deleted_at IS NULL`, id, customerID)
	if err != nil {
		return pgerr.Map(err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		err = apperr.ErrNotFound

		return err
	}

	// If the default went, the oldest remaining address takes over.
	if _, err = tx.ExecContext(ctx,
		`UPDATE addresses SET is_default = TRUE
		  WHERE id = (SELECT id FROM addresses WHERE customer_id = $1 AND deleted_at IS NULL ORDER BY id LIMIT 1)
		    AND NOT EXISTS (SELECT 1 FROM addresses WHERE customer_id = $1 AND deleted_at IS NULL AND is_default)`, customerID); err != nil {
		return pgerr.Map(err)
	}

	return tx.Commit()
}
