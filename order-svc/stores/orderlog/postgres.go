// Package orderlog is the Postgres store for an order's history.
package orderlog

import (
	"database/sql"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/pgerr"
	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/stores"
)

var _ stores.OrderLog = (*Store)(nil)

// Store implements stores.OrderLog on top of ctx.SQL.
type Store struct{}

// New returns an order-log store.
func New() *Store { return &Store{} }

// Create appends an entry to an order that exists and returns it as stored.
func (*Store) Create(ctx *gofr.Context, l *models.OrderLog) (*models.OrderLog, error) {
	var (
		out    = models.OrderLog{OrderID: l.OrderID, UserID: l.UserID, Action: l.Action, Details: l.Details}
		userID sql.NullInt64
	)

	err := ctx.SQL.QueryRowContext(ctx,
		`INSERT INTO order_logs (order_id, user_id, action, details) VALUES ($1, $2, $3, $4) RETURNING id, user_id, created_at`,
		l.OrderID, l.UserID, l.Action, string(l.Details)).Scan(&out.ID, &userID, &out.CreatedAt)
	if err != nil {
		return nil, pgerr.Map(err)
	}

	return &out, nil
}

// GetOrderID returns the id of the order with the given reference number.
func (*Store) GetOrderID(ctx *gofr.Context, referenceNumber int64) (int64, error) {
	var id int64
	if err := ctx.SQL.QueryRowContext(ctx, `SELECT id FROM orders WHERE id = $1`, referenceNumber).Scan(&id); err != nil {
		return 0, pgerr.Map(err)
	}

	return id, nil
}

// Count returns how many entries match.
func (*Store) Count(ctx *gofr.Context, f *filters.OrderLog) (int, error) {
	where, args := f.Where()

	var count int
	if err := ctx.SQL.QueryRowContext(ctx,
		`SELECT count(*) FROM order_logs JOIN orders ON orders.id = order_logs.order_id`+where, args...).Scan(&count); err != nil {
		return 0, pgerr.Map(err)
	}

	return count, nil
}

// Find returns the entries that match, newest first.
func (*Store) Find(ctx *gofr.Context, f *filters.OrderLog) ([]*models.OrderLog, error) {
	where, args := f.Where()

	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT order_logs.id, order_logs.order_id, order_logs.user_id, order_logs.action, order_logs.details, order_logs.created_at,
		        COALESCE(orders.client_id, '')
		   FROM order_logs JOIN orders ON orders.id = order_logs.order_id`+where+` ORDER BY order_logs.id DESC`, args...)
	if err != nil {
		return nil, pgerr.Map(err)
	}

	defer rows.Close()

	logs := []*models.OrderLog{}

	for rows.Next() {
		var (
			l      models.OrderLog
			userID sql.NullInt64
		)

		if err = rows.Scan(&l.ID, &l.OrderID, &userID, &l.Action, &l.Details, &l.CreatedAt, &l.ClientID); err != nil {
			return nil, err
		}

		if userID.Valid {
			l.UserID = &userID.Int64
		}

		logs = append(logs, &l)
	}

	return logs, pgerr.Map(rows.Err())
}
