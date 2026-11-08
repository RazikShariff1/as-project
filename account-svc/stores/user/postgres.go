// Package user is the Postgres store for users and their role links.
package user

import (
	"context"
	"database/sql"

	"github.com/lib/pq"
	"gofr.dev/pkg/gofr"

	"github.com/zopping/account-svc/common/apperr"
	"github.com/zopping/account-svc/common/pgerr"
	"github.com/zopping/account-svc/common/pgtypes"
	"github.com/zopping/account-svc/filters"
	"github.com/zopping/account-svc/models"
	"github.com/zopping/account-svc/stores"
)

const columns = `id, name, email, phone, status, attributes, last_login_at, created_at, updated_at`

var _ stores.User = (*Store)(nil)

// Store implements stores.User on top of ctx.SQL.
type Store struct{}

// New returns a user store.
func New() *Store { return &Store{} }

// execer is the part of ctx.SQL and *Tx that writes role links.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func scan(row interface{ Scan(...any) error }, extra ...any) (models.User, error) {
	var (
		u     models.User
		attrs []byte
		last  sql.NullTime
	)

	err := row.Scan(append([]any{&u.ID, &u.Name, &u.Email, &u.Phone, &u.Status, &attrs, &last, &u.CreatedAt, &u.UpdatedAt}, extra...)...)
	if err != nil {
		return u, err
	}

	if last.Valid {
		u.LastLoginAt = &last.Time
	}

	u.RoleIDs = []int64{}
	u.Attributes, err = pgtypes.DecodeJSON(attrs)

	return u, err
}

// setRoles replaces the user's roles. Every id must be a live role, otherwise it fails as an unknown role.
func setRoles(ctx context.Context, db execer, userID int64, roleIDs []int64) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM user_role WHERE user_id = $1`, userID); err != nil {
		return err
	}

	if len(roleIDs) == 0 {
		return nil
	}

	res, err := db.ExecContext(ctx,
		`INSERT INTO user_role (user_id, role_id)
		 SELECT $1, id FROM roles WHERE id = ANY($2::bigint[]) AND deleted_at IS NULL`, userID, pgtypes.Int64s(roleIDs))
	if err != nil {
		return err
	}

	if n, _ := res.RowsAffected(); int(n) != len(roleIDs) {
		return &apperr.InvalidRef{Constraint: "user_role_role_id_fkey"}
	}

	return nil
}

// fillRoles sets RoleIDs on each user with one query, skipping deleted roles.
func fillRoles(ctx *gofr.Context, users []models.User) error {
	if len(users) == 0 {
		return nil
	}

	ids := make([]int64, len(users))
	index := make(map[int64]int, len(users))

	for i := range users {
		ids[i] = users[i].ID
		index[users[i].ID] = i
	}

	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT ur.user_id, ur.role_id FROM user_role ur JOIN roles r ON r.id = ur.role_id AND r.deleted_at IS NULL
		  WHERE ur.user_id = ANY($1::bigint[]) ORDER BY ur.role_id`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var userID, roleID int64
		if err := rows.Scan(&userID, &roleID); err != nil {
			return err
		}

		u := &users[index[userID]]
		u.RoleIDs = append(u.RoleIDs, roleID)
	}

	return rows.Err()
}

// Create inserts a user and its role links in one transaction.
func (*Store) Create(ctx *gofr.Context, req *models.UserRequest, passwordHash string) (u models.User, err error) {
	attrs, err := pgtypes.EncodeJSON(req.Attributes)
	if err != nil {
		return u, err
	}

	tx, err := ctx.SQL.Begin()
	if err != nil {
		return u, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	u, err = scan(tx.QueryRowContext(ctx,
		`INSERT INTO users (name, email, phone, password_hash, status, attributes) VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+columns, req.Name, req.Email, req.Phone, passwordHash, req.Status, attrs))
	if err != nil {
		return u, pgerr.Map(err)
	}

	if err = setRoles(ctx, tx, u.ID, req.RoleIDs); err != nil {
		return u, pgerr.Map(err)
	}

	if err = tx.Commit(); err != nil {
		return u, err
	}

	u.RoleIDs = pgtypes.NonNil(req.RoleIDs)

	return u, nil
}

// Get fetches one user by id.
func (*Store) Get(ctx *gofr.Context, id int64) (models.User, error) {
	u, err := scan(ctx.SQL.QueryRowContext(ctx, `SELECT `+columns+` FROM users WHERE id = $1 AND deleted_at IS NULL`, id))
	if err != nil {
		return u, pgerr.Map(err)
	}

	us := []models.User{u}
	if err := fillRoles(ctx, us); err != nil {
		return u, err
	}

	return us[0], nil
}

// List returns one page of users plus the total number of matches.
func (*Store) List(ctx *gofr.Context, f *filters.UserFilter) (users []models.User, count int, err error) {
	where, args := f.Where()

	if err = ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM users`+where, args...).Scan(&count); err != nil {
		return nil, 0, pgerr.Map(err)
	}

	paging, args := f.Page(args)

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+columns+` FROM users`+where+paging, args...)
	if err != nil {
		return nil, 0, pgerr.Map(err)
	}
	defer rows.Close()

	users = []models.User{}

	for rows.Next() {
		u, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}

		users = append(users, u)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, err
	}

	return users, count, fillRoles(ctx, users)
}

// Update replaces a user's profile, status, attributes and roles in one transaction. The password is untouched.
func (*Store) Update(ctx *gofr.Context, id int64, req *models.UserRequest) (u models.User, err error) {
	attrs, err := pgtypes.EncodeJSON(req.Attributes)
	if err != nil {
		return u, err
	}

	tx, err := ctx.SQL.Begin()
	if err != nil {
		return u, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	u, err = scan(tx.QueryRowContext(ctx,
		`UPDATE users SET name = $2, email = $3, phone = $4, status = $5, attributes = $6, updated_at = now()
		  WHERE id = $1 AND deleted_at IS NULL RETURNING `+columns, id, req.Name, req.Email, req.Phone, req.Status, attrs))
	if err != nil {
		return u, pgerr.Map(err)
	}

	if err = setRoles(ctx, tx, id, req.RoleIDs); err != nil {
		return u, pgerr.Map(err)
	}

	if err = tx.Commit(); err != nil {
		return u, err
	}

	u.RoleIDs = pgtypes.NonNil(req.RoleIDs)

	return u, nil
}

// Delete soft-deletes a user and revokes their sessions in one transaction.
func (*Store) Delete(ctx *gofr.Context, id int64) (err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx, `UPDATE users SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return pgerr.Map(err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		err = apperr.ErrNotFound

		return err
	}

	if _, err = tx.ExecContext(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, id); err != nil {
		return pgerr.Map(err)
	}

	return tx.Commit()
}

// Count returns the number of live users.
func (*Store) Count(ctx *gofr.Context) (n int, err error) {
	err = ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE deleted_at IS NULL`).Scan(&n)

	return n, pgerr.Map(err)
}

// Credentials returns what login needs for an email.
func (*Store) Credentials(ctx *gofr.Context, email string) (models.Credentials, error) {
	var (
		c      models.Credentials
		locked sql.NullTime
	)

	u, err := scan(ctx.SQL.QueryRowContext(ctx,
		`SELECT `+columns+`, password_hash, failed_logins, locked_until FROM users WHERE email = $1 AND deleted_at IS NULL`, email),
		&c.PasswordHash, &c.FailedLogins, &locked)
	if err != nil {
		return c, pgerr.Map(err)
	}

	us := []models.User{u}
	if err := fillRoles(ctx, us); err != nil {
		return c, err
	}

	c.User = us[0]
	if locked.Valid {
		c.LockedUntil = &locked.Time
	}

	return c, nil
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
		`UPDATE users SET password_hash = $2, failed_logins = 0, locked_until = NULL, updated_at = now()
		  WHERE id = $1 AND deleted_at IS NULL`, id, passwordHash)
	if err != nil {
		return pgerr.Map(err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		err = apperr.ErrNotFound

		return err
	}

	if _, err = tx.ExecContext(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, id); err != nil {
		return pgerr.Map(err)
	}

	return tx.Commit()
}

// RecordFailure counts a failed login and locks the account once maxFailures is reached.
func (*Store) RecordFailure(ctx *gofr.Context, id int64, maxFailures, lockSeconds int) (locked bool, err error) {
	err = ctx.SQL.QueryRowContext(ctx,
		`UPDATE users
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
		`UPDATE users SET failed_logins = 0, locked_until = NULL, last_login_at = now() WHERE id = $1`, id)

	return pgerr.Map(err)
}

// ClearExpiredLock resets the failure count of an account whose lock has run out.
func (*Store) ClearExpiredLock(ctx *gofr.Context, id int64) error {
	_, err := ctx.SQL.ExecContext(ctx,
		`UPDATE users SET failed_logins = 0, locked_until = NULL
		  WHERE id = $1 AND locked_until IS NOT NULL AND locked_until <= now()`, id)

	return pgerr.Map(err)
}
