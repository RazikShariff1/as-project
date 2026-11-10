// Package session is the Postgres store for login sessions.
package session

import (
	"database/sql"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/customer-svc/common/pgerr"
	"github.com/zopping/customer-svc/models"
	"github.com/zopping/customer-svc/stores"
)

var _ stores.Session = (*Store)(nil)

// Store implements stores.Session on top of ctx.SQL.
type Store struct{}

// New returns a session store.
func New() *Store { return &Store{} }

// Create stores a session and returns its expiry, computed from the database clock.
func (*Store) Create(ctx *gofr.Context, customerID int64, tokenHash string, ttlSeconds int, userAgent, ip string) (time.Time, error) {
	var expires time.Time

	err := ctx.SQL.QueryRowContext(ctx,
		`INSERT INTO sessions (customer_id, token_hash, user_agent, ip, expires_at)
		 VALUES ($1, $2, $3, $4, now() + make_interval(secs => $5::double precision)) RETURNING expires_at`,
		customerID, tokenHash, userAgent, ip, ttlSeconds).Scan(&expires)

	return expires, pgerr.Map(err)
}

// Lookup returns the customer behind a live token (not expired, not revoked, customer not deleted).
func (*Store) Lookup(ctx *gofr.Context, tokenHash string) (models.SessionLookup, error) {
	var (
		l    models.SessionLookup
		last sql.NullTime
	)

	c := &l.Customer

	err := ctx.SQL.QueryRowContext(ctx,
		`SELECT s.id, s.expires_at, c.id, c.name, c.email, c.phone, c.status, c.last_login_at, c.created_at, c.updated_at
		   FROM sessions s JOIN customers c ON c.id = s.customer_id
		  WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now() AND c.deleted_at IS NULL`, tokenHash).
		Scan(&l.SessionID, &l.ExpiresAt, &c.ID, &c.Name, &c.Email, &c.Phone, &c.Status, &last, &c.CreatedAt, &c.UpdatedAt)
	if last.Valid {
		c.LastLoginAt = &last.Time
	}

	return l, pgerr.Map(err)
}

// Revoke ends a session; it succeeds whether or not the token was live.
func (*Store) Revoke(ctx *gofr.Context, tokenHash string) error {
	_, err := ctx.SQL.ExecContext(ctx, `UPDATE sessions SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash)

	return pgerr.Map(err)
}
