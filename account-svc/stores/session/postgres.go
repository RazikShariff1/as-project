// Package session is the Postgres store for login sessions.
package session

import (
	"database/sql"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/account-svc/common/pgerr"
	"github.com/zopping/account-svc/common/pgtypes"
	"github.com/zopping/account-svc/models"
	"github.com/zopping/account-svc/stores"
)

var _ stores.Session = (*Store)(nil)

// Store implements stores.Session on top of ctx.SQL.
type Store struct{}

// New returns a session store.
func New() *Store { return &Store{} }

// Create stores a session and returns its expiry, computed from the database clock.
func (*Store) Create(ctx *gofr.Context, userID int64, tokenHash string, ttlSeconds int, userAgent, ip string) (time.Time, error) {
	var expires time.Time

	err := ctx.SQL.QueryRowContext(ctx,
		`INSERT INTO sessions (user_id, token_hash, user_agent, ip, expires_at)
		 VALUES ($1, $2, $3, $4, now() + make_interval(secs => $5::double precision)) RETURNING expires_at`,
		userID, tokenHash, userAgent, ip, ttlSeconds).Scan(&expires)

	return expires, pgerr.Map(err)
}

// Lookup returns the user behind a live token (not expired, not revoked, user not deleted).
func (*Store) Lookup(ctx *gofr.Context, tokenHash string) (models.SessionLookup, error) {
	var (
		l     models.SessionLookup
		attrs []byte
		last  sql.NullTime
	)

	u := &l.User

	err := ctx.SQL.QueryRowContext(ctx,
		`SELECT s.id, s.expires_at, u.id, u.name, u.email, u.phone, u.status, u.attributes, u.last_login_at, u.created_at, u.updated_at
		   FROM sessions s JOIN users u ON u.id = s.user_id
		  WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now() AND u.deleted_at IS NULL`, tokenHash).
		Scan(&l.SessionID, &l.ExpiresAt, &u.ID, &u.Name, &u.Email, &u.Phone, &u.Status, &attrs, &last, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return l, pgerr.Map(err)
	}

	if last.Valid {
		u.LastLoginAt = &last.Time
	}

	if u.Attributes, err = pgtypes.DecodeJSON(attrs); err != nil {
		return l, err
	}

	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT ur.role_id FROM user_role ur JOIN roles r ON r.id = ur.role_id AND r.deleted_at IS NULL
		  WHERE ur.user_id = $1 ORDER BY ur.role_id`, u.ID)
	if err != nil {
		return l, pgerr.Map(err)
	}
	defer rows.Close()

	u.RoleIDs = []int64{}

	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return l, err
		}

		u.RoleIDs = append(u.RoleIDs, id)
	}

	return l, rows.Err()
}

// Revoke ends a session; it succeeds whether or not the token was live.
func (*Store) Revoke(ctx *gofr.Context, tokenHash string) error {
	_, err := ctx.SQL.ExecContext(ctx, `UPDATE sessions SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash)

	return pgerr.Map(err)
}

// RevokeUser ends every live session of a user.
func (*Store) RevokeUser(ctx *gofr.Context, userID int64) error {
	_, err := ctx.SQL.ExecContext(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)

	return pgerr.Map(err)
}
