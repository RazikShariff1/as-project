// Package email is the Postgres store for the outbox.
package email

import (
	"database/sql"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/communication-svc/common/pgerr"
	"github.com/zopping/communication-svc/filters"
	"github.com/zopping/communication-svc/models"
	"github.com/zopping/communication-svc/stores"
)

const columns = `id, type, language, to_addr, cc_addr, bcc_addr, subject, status, attempts, last_error, sent_at, created_at`

var _ stores.Email = (*Store)(nil)

// Store implements stores.Email on top of ctx.SQL.
type Store struct{}

// New returns an email store.
func New() *Store { return &Store{} }

func scan(row interface{ Scan(...any) error }) (models.Email, error) {
	var (
		e    models.Email
		sent sql.NullTime
	)

	err := row.Scan(&e.ID, &e.Type, &e.Language, &e.To, &e.Cc, &e.Bcc, &e.Subject, &e.Status, &e.Attempts, &e.LastError, &sent, &e.CreatedAt)
	if sent.Valid {
		e.SentAt = &sent.Time
	}

	return e, err
}

// Enqueue stores a rendered email, queued for delivery.
func (*Store) Enqueue(ctx *gofr.Context, o *models.Outbound) (models.Email, error) {
	e, err := scan(ctx.SQL.QueryRowContext(ctx,
		`INSERT INTO emails (type, language, to_addr, cc_addr, bcc_addr, subject, body) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING `+columns,
		o.Type, o.Language, o.To, o.Cc, o.Bcc, o.Subject, o.Body))

	return e, pgerr.Map(err)
}

// Claim takes due emails for one worker; FOR UPDATE SKIP LOCKED keeps concurrent workers off each other's rows.
func (*Store) Claim(ctx *gofr.Context, limit, lockSeconds int) ([]models.Outbound, error) {
	rows, err := ctx.SQL.QueryContext(ctx, `
UPDATE emails SET status = 'sending', attempts = attempts + 1, updated_at = now(),
       locked_until = now() + make_interval(secs => $2)
WHERE id IN (
    SELECT id FROM emails
    WHERE (status = 'queued' AND send_after <= now()) OR (status = 'sending' AND locked_until < now())
    ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED)
RETURNING id, type, language, to_addr, cc_addr, bcc_addr, subject, body, attempts`, limit, lockSeconds)
	if err != nil {
		return nil, pgerr.Map(err)
	}
	defer rows.Close()

	var out []models.Outbound

	for rows.Next() {
		var o models.Outbound
		if err := rows.Scan(&o.ID, &o.Type, &o.Language, &o.To, &o.Cc, &o.Bcc, &o.Subject, &o.Body, &o.Attempts); err != nil {
			return nil, err
		}

		out = append(out, o)
	}

	return out, rows.Err()
}

// MarkSent records a delivered email.
func (*Store) MarkSent(ctx *gofr.Context, id int64) error {
	_, err := ctx.SQL.ExecContext(ctx,
		`UPDATE emails SET status = 'sent', sent_at = now(), last_error = '', locked_until = NULL, updated_at = now() WHERE id = $1`, id)

	return pgerr.Map(err)
}

// MarkRetry queues the email again after a back-off.
func (*Store) MarkRetry(ctx *gofr.Context, id int64, delaySeconds int, reason string) error {
	_, err := ctx.SQL.ExecContext(ctx, `UPDATE emails SET status = 'queued', last_error = $2, locked_until = NULL, updated_at = now(),
       send_after = now() + make_interval(secs => $3) WHERE id = $1`, id, reason, delaySeconds)

	return pgerr.Map(err)
}

// MarkFailed gives up on the email.
func (*Store) MarkFailed(ctx *gofr.Context, id int64, reason string) error {
	_, err := ctx.SQL.ExecContext(ctx,
		`UPDATE emails SET status = 'failed', last_error = $2, locked_until = NULL, updated_at = now() WHERE id = $1`, id, reason)

	return pgerr.Map(err)
}

// Get fetches one email by id.
func (*Store) Get(ctx *gofr.Context, id int64) (models.Email, error) {
	e, err := scan(ctx.SQL.QueryRowContext(ctx, `SELECT `+columns+` FROM emails WHERE id = $1`, id))

	return e, pgerr.Map(err)
}

// List returns one page of emails, newest first, plus the total number of matches.
func (*Store) List(ctx *gofr.Context, f *filters.EmailFilter) (emails []models.Email, count int, err error) {
	where, args := f.Where()

	if err = ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM emails`+where, args...).Scan(&count); err != nil {
		return nil, 0, pgerr.Map(err)
	}

	paging, args := f.Page(args)

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+columns+` FROM emails`+where+` ORDER BY id DESC`+paging, args...)
	if err != nil {
		return nil, 0, pgerr.Map(err)
	}
	defer rows.Close()

	emails = []models.Email{}

	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}

		emails = append(emails, e)
	}

	return emails, count, rows.Err()
}
