// Package order is the Postgres store for orders. Every write is one transaction that touches orders, order_items and
// order_logs together, so an order and its history can never disagree.
package order

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/apperr"
	"github.com/zopping/order-svc/common/pgerr"
	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/stores"
)

var _ stores.Order = (*Store)(nil)

const orderColumns = `id, order_ref, customer_id, status, payment_method, payment_status, subtotal, discount_total, total,
	delivery_address, notes, cancel_reason, created_at, updated_at`

// Store implements stores.Order on top of ctx.SQL.
type Store struct{}

// New returns an order store.
func New() *Store { return &Store{} }

// txn is the part of a GoFr transaction this store uses.
type txn interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface{ Scan(dest ...any) error }

func scanOrder(s scanner) (models.Order, error) {
	var (
		o    models.Order
		addr []byte
	)

	err := s.Scan(&o.ID, &o.OrderRef, &o.CustomerID, &o.Status, &o.PaymentMethod, &o.PaymentStatus, &o.Subtotal, &o.DiscountTotal,
		&o.Total, &addr, &o.Notes, &o.CancelReason, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return o, err
	}

	if err = json.Unmarshal(addr, &o.DeliveryAddress); err != nil {
		return o, fmt.Errorf("order %d has an unreadable delivery address: %w", o.ID, err)
	}

	o.Items = []models.OrderItem{}

	return o, nil
}

// Create stores the order, its items and the PLACED log entry.
func (*Store) Create(ctx *gofr.Context, o *models.Order, userID *int64) (res models.Order, err error) {
	addr, err := json.Marshal(o.DeliveryAddress)
	if err != nil {
		return res, err
	}

	tx, err := ctx.SQL.Begin()
	if err != nil {
		return res, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var id int64

	err = tx.QueryRowContext(ctx,
		`INSERT INTO orders (order_ref, customer_id, payment_method, subtotal, discount_total, total, delivery_address, notes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		o.OrderRef, o.CustomerID, o.PaymentMethod, o.Subtotal, o.DiscountTotal, o.Total, addr, o.Notes).Scan(&id)
	if err != nil {
		return res, pgerr.Map(err)
	}

	if err = insertItems(ctx, tx, id, o.Items); err != nil {
		return res, err
	}

	if err = writeLog(ctx, tx, id, models.LogStatus, "", models.StatusPlaced, "", userID); err != nil {
		return res, err
	}

	if err = tx.Commit(); err != nil {
		return res, err
	}

	return (&Store{}).Get(ctx, id)
}

func insertItems(ctx *gofr.Context, tx txn, orderID int64, items []models.OrderItem) error {
	for _, it := range items {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO order_items (order_id, variant_id, seller_id, quantity, mrp, discount, unit_price, line_total, tax, hsn_code)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			orderID, it.VariantID, it.SellerID, it.Quantity, it.MRP, it.Discount, it.UnitPrice, it.LineTotal, it.Tax, it.HSNCode)
		if err != nil {
			return pgerr.Map(err)
		}
	}

	return nil
}

func writeLog(ctx *gofr.Context, tx txn, orderID int64, kind, from, to, note string, userID *int64) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO order_logs (order_id, kind, from_value, to_value, note, user_id) VALUES ($1, $2, $3, $4, $5, $6)`,
		orderID, kind, from, to, note, userID)

	return pgerr.Map(err)
}

// Get returns an order with its items.
func (*Store) Get(ctx *gofr.Context, id int64) (models.Order, error) {
	o, err := scanOrder(ctx.SQL.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM orders WHERE id = $1`, id))
	if err != nil {
		return o, pgerr.Map(err)
	}

	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT id, variant_id, seller_id, quantity, mrp, discount, unit_price, line_total, tax, hsn_code
		   FROM order_items WHERE order_id = $1 ORDER BY id`, id)
	if err != nil {
		return o, pgerr.Map(err)
	}

	defer rows.Close()

	for rows.Next() {
		var it models.OrderItem
		if err = rows.Scan(&it.ID, &it.VariantID, &it.SellerID, &it.Quantity, &it.MRP, &it.Discount, &it.UnitPrice, &it.LineTotal,
			&it.Tax, &it.HSNCode); err != nil {
			return o, err
		}

		o.Items = append(o.Items, it)
	}

	return o, rows.Err()
}

// List returns a page of orders without their items, and how many orders match.
func (*Store) List(ctx *gofr.Context, f *filters.OrderFilter) ([]models.Order, int, error) {
	where, args := f.Where()

	var count int
	if err := ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM orders`+where, args...).Scan(&count); err != nil {
		return nil, 0, pgerr.Map(err)
	}

	paging, args := f.Page(args)

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+orderColumns+` FROM orders`+where+paging, args...)
	if err != nil {
		return nil, 0, pgerr.Map(err)
	}

	defer rows.Close()

	orders := []models.Order{}

	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, 0, err
		}

		orders = append(orders, o)
	}

	return orders, count, rows.Err()
}

// Transition moves an order to status to if it is currently in one of the allowed statuses.
func (s *Store) Transition(ctx *gofr.Context, id int64, allowed []string, to, note string, userID *int64) (res models.Order, err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return res, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var current, method string
	if err = tx.QueryRowContext(ctx, `SELECT status, payment_method FROM orders WHERE id = $1 FOR UPDATE`, id).
		Scan(&current, &method); err != nil {
		return res, pgerr.Map(err)
	}

	if !slices.Contains(allowed, current) {
		return res, &apperr.StateConflict{Reason: fmt.Sprintf("the order is %s; it cannot move to %s", current, to)}
	}

	reason := ""
	if to == models.StatusCancelled {
		reason = note
	}

	if _, err = tx.ExecContext(ctx, `UPDATE orders SET status = $2, cancel_reason = $3, updated_at = now() WHERE id = $1`,
		id, to, reason); err != nil {
		return res, pgerr.Map(err)
	}

	if err = writeLog(ctx, tx, id, models.LogStatus, current, to, note, userID); err != nil {
		return res, err
	}

	if to == models.StatusDelivered && method == models.MethodCOD {
		if err = markCODPaid(ctx, tx, id, userID); err != nil {
			return res, err
		}
	}

	if err = tx.Commit(); err != nil {
		return res, err
	}

	return s.Get(ctx, id)
}

// markCODPaid records cash collected on delivery.
func markCODPaid(ctx *gofr.Context, tx txn, id int64, userID *int64) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE orders SET payment_status = $2, updated_at = now() WHERE id = $1 AND payment_status = $3`,
		id, models.PaymentPaid, models.PaymentPending)
	if err != nil {
		return pgerr.Map(err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}

	return writeLog(ctx, tx, id, models.LogPayment, models.PaymentPending, models.PaymentPaid, "cash on delivery", userID)
}

// SetPaymentStatus records the payment outcome. Repeating the current status changes nothing.
func (s *Store) SetPaymentStatus(ctx *gofr.Context, id int64, status, note string, userID *int64) (res models.Order, err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return res, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var current string
	if err = tx.QueryRowContext(ctx, `SELECT payment_status FROM orders WHERE id = $1 FOR UPDATE`, id).Scan(&current); err != nil {
		return res, pgerr.Map(err)
	}

	if current != status {
		if _, err = tx.ExecContext(ctx, `UPDATE orders SET payment_status = $2, updated_at = now() WHERE id = $1`, id, status); err != nil {
			return res, pgerr.Map(err)
		}

		if err = writeLog(ctx, tx, id, models.LogPayment, current, status, note, userID); err != nil {
			return res, err
		}
	}

	if err = tx.Commit(); err != nil {
		return res, err
	}

	return s.Get(ctx, id)
}

// Logs returns an order's history, oldest first.
func (*Store) Logs(ctx *gofr.Context, id int64) ([]models.OrderLog, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT id, kind, from_value, to_value, note, user_id, created_at FROM order_logs WHERE order_id = $1 ORDER BY id`, id)
	if err != nil {
		return nil, pgerr.Map(err)
	}

	defer rows.Close()

	logs := []models.OrderLog{}

	for rows.Next() {
		var (
			l      models.OrderLog
			userID sql.NullInt64
		)

		if err = rows.Scan(&l.ID, &l.Kind, &l.From, &l.To, &l.Note, &userID, &l.CreatedAt); err != nil {
			return nil, err
		}

		if userID.Valid {
			l.UserID = &userID.Int64
		}

		logs = append(logs, l)
	}

	return logs, rows.Err()
}
