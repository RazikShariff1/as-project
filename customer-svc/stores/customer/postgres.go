// Package customer is the Postgres store for customers.
package customer

import (
	"database/sql"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/customer-svc/common/apperr"
	"github.com/zopping/customer-svc/common/pgerr"
	"github.com/zopping/customer-svc/filters"
	"github.com/zopping/customer-svc/models"
	"github.com/zopping/customer-svc/stores"
)

const columns = `id, name, email, phone, status, last_login_at, created_at, updated_at`

var _ stores.Customer = (*Store)(nil)

// Store implements stores.Customer on top of ctx.SQL.
type Store struct{}

// New returns a customer store.
func New() *Store { return &Store{} }

func scan(row interface{ Scan(...any) error }, extra ...any) (models.Customer, error) {
	var (
		c    models.Customer
		last sql.NullTime
	)

	err := row.Scan(append([]any{&c.ID, &c.Name, &c.Email, &c.Phone, &c.Status, &last, &c.CreatedAt, &c.UpdatedAt}, extra...)...)
	if last.Valid {
		c.LastLoginAt = &last.Time
	}

	return c, err
}

// Create stores a customer; a duplicate live email is a conflict.
func (*Store) Create(ctx *gofr.Context, req *models.RegisterRequest, passwordHash string) (models.Customer, error) {
	c, err := scan(ctx.SQL.QueryRowContext(ctx,
		`INSERT INTO customers (name, email, phone, password_hash) VALUES ($1, $2, $3, $4) RETURNING `+columns,
		req.Name, req.Email, req.Phone, passwordHash))

	return c, pgerr.Map(err)
}

// Get fetches one customer by id.
func (*Store) Get(ctx *gofr.Context, id int64) (models.Customer, error) {
	c, err := scan(ctx.SQL.QueryRowContext(ctx, `SELECT `+columns+` FROM customers WHERE id = $1 AND deleted_at IS NULL`, id))

	return c, pgerr.Map(err)
}

// List returns one page of customers plus the total number of matches.
func (*Store) List(ctx *gofr.Context, f *filters.CustomerFilter) (customers []models.Customer, count int, err error) {
	where, args := f.Where()

	if err = ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM customers`+where, args...).Scan(&count); err != nil {
		return nil, 0, pgerr.Map(err)
	}

	paging, args := f.Page(args)

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+columns+` FROM customers`+where+paging, args...)
	if err != nil {
		return nil, 0, pgerr.Map(err)
	}
	defer rows.Close()

	customers = []models.Customer{}

	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}

		customers = append(customers, c)
	}

	return customers, count, rows.Err()
}

// UpdateProfile replaces a customer's name and phone.
func (*Store) UpdateProfile(ctx *gofr.Context, id int64, req *models.ProfileRequest) (models.Customer, error) {
	c, err := scan(ctx.SQL.QueryRowContext(ctx,
		`UPDATE customers SET name = $2, phone = $3, updated_at = now() WHERE id = $1 AND deleted_at IS NULL RETURNING `+columns,
		id, req.Name, req.Phone))

	return c, pgerr.Map(err)
}

// SetStatus changes a customer's status. Suspending revokes their sessions in the same transaction, so a suspended
// customer is out on their next call.
func (*Store) SetStatus(ctx *gofr.Context, id int64, status string) (c models.Customer, err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return c, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	c, err = scan(tx.QueryRowContext(ctx,
		`UPDATE customers SET status = $2, updated_at = now() WHERE id = $1 AND deleted_at IS NULL RETURNING `+columns, id, status))
	if err != nil {
		return c, pgerr.Map(err)
	}

	if status != models.StatusActive {
		if _, err = tx.ExecContext(ctx, `UPDATE sessions SET revoked_at = now() WHERE customer_id = $1 AND revoked_at IS NULL`, id); err != nil {
			return c, pgerr.Map(err)
		}
	}

	return c, tx.Commit()
}

// Credentials returns what login needs for an email.
func (*Store) Credentials(ctx *gofr.Context, email string) (models.Credentials, error) {
	var (
		cr     models.Credentials
		locked sql.NullTime
	)

	c, err := scan(ctx.SQL.QueryRowContext(ctx,
		`SELECT `+columns+`, password_hash, failed_logins, locked_until FROM customers WHERE email = $1 AND deleted_at IS NULL`, email),
		&cr.PasswordHash, &cr.FailedLogins, &locked)
	if err != nil {
		return cr, pgerr.Map(err)
	}

	cr.Customer = c
	if locked.Valid {
		cr.LockedUntil = &locked.Time
	}

	return cr, nil
}

// SetPassword replaces the hash, clears any lockout and revokes every session in one transaction.
func (*Store) SetPassword(ctx *gofr.Context, id int64, passwordHash string) (err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx,
		`UPDATE customers SET password_hash = $2, failed_logins = 0, locked_until = NULL, updated_at = now()
		  WHERE id = $1 AND deleted_at IS NULL`, id, passwordHash)
	if err != nil {
		return pgerr.Map(err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		err = apperr.ErrNotFound

		return err
	}

	if _, err = tx.ExecContext(ctx, `UPDATE sessions SET revoked_at = now() WHERE customer_id = $1 AND revoked_at IS NULL`, id); err != nil {
		return pgerr.Map(err)
	}

	return tx.Commit()
}

// RecordFailure counts a failed login and locks the account once maxFailures is reached.
func (*Store) RecordFailure(ctx *gofr.Context, id int64, maxFailures, lockSeconds int) (locked bool, err error) {
	err = ctx.SQL.QueryRowContext(ctx,
		`UPDATE customers
		    SET failed_logins = failed_logins + 1,
		        locked_until = CASE WHEN failed_logins + 1 >= $2 THEN now() + make_interval(secs => $3::double precision)
		                            ELSE locked_until END
		  WHERE id = $1
		 RETURNING failed_logins >= $2`, id, maxFailures, lockSeconds).Scan(&locked)

	return locked, pgerr.Map(err)
}

// RecordSuccess resets the failure count and stamps the login time.
func (*Store) RecordSuccess(ctx *gofr.Context, id int64) error {
	_, err := ctx.SQL.ExecContext(ctx,
		`UPDATE customers SET failed_logins = 0, locked_until = NULL, last_login_at = now() WHERE id = $1`, id)

	return pgerr.Map(err)
}

// ClearExpiredLock resets the failure count of an account whose lock has run out.
func (*Store) ClearExpiredLock(ctx *gofr.Context, id int64) error {
	_, err := ctx.SQL.ExecContext(ctx,
		`UPDATE customers SET failed_logins = 0, locked_until = NULL
		  WHERE id = $1 AND locked_until IS NOT NULL AND locked_until <= now()`, id)

	return pgerr.Map(err)
}
