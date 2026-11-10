// Package order is the Postgres store for orders. Every write is one transaction that touches orders, order_items and
// order_logs together, so an order and its history can never disagree. Reads cost one query: an order's items come back inside
// the same statement as the order.
package order

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/apperr"
	"github.com/zopping/order-svc/common/pgerr"
	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/stores"
)

var _ stores.Order = (*Store)(nil)

const orderColumns = `id, order_ref, client_id, customer_id, status, payment_method, payment_status, subtotal, discount_total, total,
	delivery_address, notes, cancel_reason, meta_data, stale_refs, created_at, completed_at, updated_at`

// itemsJSON aggregates an order's lines as JSON whose keys are the models.OrderItem json tags, so it decodes straight into them.
const itemsJSON = `COALESCE((SELECT json_agg(json_build_object('id', i.id, 'variantId', i.variant_id, 'sellerId', i.seller_id,
	'quantity', i.quantity, 'unit', i.unit, 'status', i.status, 'mrp', i.mrp, 'discount', i.discount, 'unitPrice', i.unit_price,
	'lineTotal', i.line_total, 'tax', i.tax, 'hsnCode', i.hsn_code) ORDER BY i.id)
	FROM order_items i WHERE i.order_id = orders.id), '[]'::json)`

// Store implements stores.Order on top of ctx.SQL.
type Store struct{}

// New returns an order store.
func New() *Store { return &Store{} }

// querier is the part of ctx.SQL and of a transaction that reads one row.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// txn is the part of a GoFr transaction this store uses.
type txn interface {
	querier
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface{ Scan(dest ...any) error }

// scanOrder reads orderColumns, then any extra destinations (the items JSON or a window count).
func scanOrder(s scanner, extra ...any) (models.Order, error) {
	var (
		o      models.Order
		addr   []byte
		meta   []byte
		client sql.NullString
		done   sql.NullTime
	)

	dest := append([]any{&o.ReferenceNumber, &o.OrderRef, &client, &o.CustomerID, &o.Status, &o.PaymentMethod, &o.PaymentStatus,
		&o.Subtotal, &o.DiscountTotal, &o.Total, &addr, &o.Notes, &o.CancelReason, &meta, pq.Array(&o.StaleRefs),
		&o.CreatedAt, &done, &o.UpdatedAt}, extra...)

	if err := s.Scan(dest...); err != nil {
		return o, err
	}

	o.ClientID = client.String

	if done.Valid {
		o.CompletedAt = &done.Time
	}

	if err := json.Unmarshal(addr, &o.DeliveryAddress); err != nil {
		return o, fmt.Errorf("order %d has an unreadable delivery address: %w", o.ReferenceNumber, err)
	}

	o.MetaData = map[string]any{}
	if len(meta) > 0 {
		if err := json.Unmarshal(meta, &o.MetaData); err != nil {
			return o, fmt.Errorf("order %d has unreadable metadata: %w", o.ReferenceNumber, err)
		}
	}

	o.Items = []models.OrderItem{}

	return o, nil
}

// getOrder reads one order with its items in a single query, on the pool or inside a transaction.
func getOrder(ctx *gofr.Context, q querier, where string, args ...any) (models.Order, error) {
	var items []byte

	o, err := scanOrder(q.QueryRowContext(ctx, `SELECT `+orderColumns+`, `+itemsJSON+` FROM orders WHERE `+where, args...), &items)
	if err != nil {
		return o, pgerr.Map(err)
	}

	if err = json.Unmarshal(items, &o.Items); err != nil {
		return o, fmt.Errorf("order %d has unreadable items: %w", o.ReferenceNumber, err)
	}

	return o, nil
}

// Retrieve returns an order with its items.
func (*Store) Retrieve(ctx *gofr.Context, ref int64) (*models.Order, error) {
	o, err := getOrder(ctx, ctx.SQL, `id = $1`, ref)
	if err != nil {
		return nil, err
	}

	return &o, nil
}

// GetByClientID returns the customer's order placed with the given client id.
func (*Store) GetByClientID(ctx *gofr.Context, customerID int64, clientID string) (*models.Order, error) {
	o, err := getOrder(ctx, ctx.SQL, `customer_id = $1 AND client_id = $2`, customerID, clientID)
	if err != nil {
		return nil, err
	}

	return &o, nil
}

// Index returns a page of orders with their items.
func (*Store) Index(ctx *gofr.Context, f *filters.OrderDetails, limit, offset int) ([]*models.Order, error) {
	where, args := f.Where()
	paging, args := f.Page(args, limit, offset)

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+orderColumns+`, `+itemsJSON+` FROM orders`+where+paging, args...)
	if err != nil {
		return nil, pgerr.Map(err)
	}

	defer rows.Close()

	orders := []*models.Order{}

	for rows.Next() {
		var items []byte

		o, err := scanOrder(rows, &items)
		if err != nil {
			return nil, err
		}

		if err = json.Unmarshal(items, &o.Items); err != nil {
			return nil, fmt.Errorf("order %d has unreadable items: %w", o.ReferenceNumber, err)
		}

		orders = append(orders, &o)
	}

	return orders, pgerr.Map(rows.Err())
}

// Count returns how many orders match the filter.
func (*Store) Count(ctx *gofr.Context, f *filters.OrderDetails) (int, error) {
	where, args := f.Where()

	var count int
	if err := ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM orders`+where, args...).Scan(&count); err != nil {
		return 0, pgerr.Map(err)
	}

	return count, nil
}

// Create stores the order, its items and its logs.
func (*Store) Create(ctx *gofr.Context, o *models.Order) (res *models.Order, err error) {
	addr, err := json.Marshal(o.DeliveryAddress)
	if err != nil {
		return nil, err
	}

	meta, err := json.Marshal(o.MetaData)
	if err != nil {
		return nil, err
	}

	tx, err := ctx.SQL.Begin()
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var client any
	if o.ClientID != "" {
		client = o.ClientID
	}

	created := o.DeepCopy()

	err = tx.QueryRowContext(ctx,
		`INSERT INTO orders (order_ref, client_id, customer_id, payment_method, subtotal, discount_total, total, delivery_address,
		                     notes, meta_data)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id, status, payment_status, created_at, updated_at`,
		o.OrderRef, client, o.CustomerID, o.PaymentMethod, o.Subtotal, o.DiscountTotal, o.Total, string(addr), o.Notes, string(meta)).
		Scan(&created.ReferenceNumber, &created.Status, &created.PaymentStatus, &created.CreatedAt, &created.UpdatedAt)
	if err != nil {
		return nil, pgerr.Map(err)
	}

	if err = insertItems(ctx, tx, created.ReferenceNumber, created.Items); err != nil {
		return nil, err
	}

	if err = insertLogs(ctx, tx, created.ReferenceNumber, created.OrderLogs); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	created.OrderLogs = nil

	return created, nil
}

// insertItems writes every line in one statement and fills in their ids.
func insertItems(ctx *gofr.Context, tx txn, orderID int64, items []models.OrderItem) error {
	args := []any{orderID}
	values := make([]string, 0, len(items))

	for _, it := range items {
		n := len(args)

		values = append(values, fmt.Sprintf("($1, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			n+1, n+2, n+3, n+4, n+5, n+6, n+7, n+8, n+9, n+10))
		args = append(args, it.VariantID, it.SellerID, it.Quantity, it.Unit, it.MRP, it.Discount, it.UnitPrice, it.LineTotal,
			it.Tax, it.HSNCode)
	}

	rows, err := tx.QueryContext(ctx,
		`INSERT INTO order_items (order_id, variant_id, seller_id, quantity, unit, mrp, discount, unit_price, line_total, tax, hsn_code)
		 VALUES `+strings.Join(values, ", ")+` RETURNING id, variant_id, seller_id`, args...)
	if err != nil {
		return pgerr.Map(err)
	}

	defer rows.Close()

	type key struct{ variantID, sellerID int64 }

	ids := make(map[key]int64, len(items))

	for rows.Next() {
		var id, variantID, sellerID int64
		if err = rows.Scan(&id, &variantID, &sellerID); err != nil {
			return err
		}

		ids[key{variantID, sellerID}] = id
	}

	if err = rows.Err(); err != nil {
		return pgerr.Map(err)
	}

	for i := range items {
		items[i].ID = ids[key{items[i].VariantID, items[i].SellerID}]
		items[i].Status = models.ItemPending
	}

	return nil
}

// insertLogs appends the history rows that are not saved yet (ID 0), in one statement.
func insertLogs(ctx *gofr.Context, tx txn, orderID int64, logs []*models.OrderLog) error {
	args := []any{orderID}
	values := make([]string, 0, len(logs))

	for _, l := range logs {
		if l.ID != 0 {
			continue
		}

		n := len(args)

		values = append(values, fmt.Sprintf("($1, $%d, $%d, $%d)", n+1, n+2, n+3))
		args = append(args, l.UserID, l.Action, string(l.Details))
	}

	if len(values) == 0 {
		return nil
	}

	_, err := tx.ExecContext(ctx, `INSERT INTO order_logs (order_id, user_id, action, details) VALUES `+strings.Join(values, ", "), args...)

	return pgerr.Map(err)
}

// Update locks the order row, checks nobody saved it since it was read, and writes the order, its items and its new logs.
func (*Store) Update(ctx *gofr.Context, ref int64, o *models.Order) (res *models.Order, err error) {
	addr, err := json.Marshal(o.DeliveryAddress)
	if err != nil {
		return nil, err
	}

	meta, err := json.Marshal(o.MetaData)
	if err != nil {
		return nil, err
	}

	tx, err := ctx.SQL.Begin()
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var version time.Time

	if err = tx.QueryRowContext(ctx, `SELECT updated_at FROM orders WHERE id = $1 FOR UPDATE`, ref).Scan(&version); err != nil {
		return nil, pgerr.Map(err)
	}

	if !version.Equal(o.UpdatedAt) {
		return nil, &apperr.StateConflict{Reason: "the order was changed by another request; read it and try again"}
	}

	if _, err = tx.ExecContext(ctx,
		`UPDATE orders SET status = $2, payment_status = $3, subtotal = $4, discount_total = $5, total = $6, delivery_address = $7,
		        notes = $8, cancel_reason = $9, meta_data = $10, order_ref = $11, stale_refs = $12, completed_at = $13, updated_at = now()
		  WHERE id = $1`,
		ref, o.Status, o.PaymentStatus, o.Subtotal, o.DiscountTotal, o.Total, string(addr), o.Notes, o.CancelReason, string(meta),
		o.OrderRef, pq.Array(o.StaleRefs), o.CompletedAt); err != nil {
		return nil, pgerr.Map(err)
	}

	if err = updateItems(ctx, tx, ref, o.Items); err != nil {
		return nil, err
	}

	if err = insertLogs(ctx, tx, ref, o.OrderLogs); err != nil {
		return nil, err
	}

	stored, err := getOrder(ctx, tx, `id = $1`, ref)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return &stored, nil
}

// updateItems writes the lines whose quantity, total or status differ from what is stored, in one statement.
func updateItems(ctx *gofr.Context, tx txn, ref int64, items []models.OrderItem) error {
	if len(items) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(items))
	quantities := make([]int64, 0, len(items))
	totals := make([]float64, 0, len(items))
	statuses := make([]string, 0, len(items))

	for _, it := range items {
		ids = append(ids, it.ID)
		quantities = append(quantities, it.Quantity)
		totals = append(totals, it.LineTotal)
		statuses = append(statuses, it.Status)
	}

	_, err := tx.ExecContext(ctx,
		`UPDATE order_items i SET quantity = v.q, line_total = v.lt, status = v.st
		   FROM unnest($2::bigint[], $3::bigint[], $4::numeric[], $5::text[]) AS v(id, q, lt, st)
		  WHERE i.id = v.id AND i.order_id = $1 AND (i.quantity, i.line_total, i.status) IS DISTINCT FROM (v.q, v.lt, v.st)`,
		ref, pq.Array(ids), pq.Array(quantities), pq.Array(totals), pq.Array(statuses))

	return pgerr.Map(err)
}

// ClearStaleRef forgets a replaced reservation once it has been released.
func (*Store) ClearStaleRef(ctx *gofr.Context, ref int64, staleRef string) error {
	_, err := ctx.SQL.ExecContext(ctx, `UPDATE orders SET stale_refs = array_remove(stale_refs, $2) WHERE id = $1`, ref, staleRef)

	return pgerr.Map(err)
}
