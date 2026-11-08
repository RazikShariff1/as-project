// Package reservation is the Postgres store for stock reservations.
//
// Every method is one transaction that touches stock_reservations, stock and stock_movements together, so a
// reservation and the stock it holds can never disagree. Stock rows are always locked in ascending variant id.
package reservation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/inventory-svc/common/apperr"
	"github.com/zopping/inventory-svc/common/pgerr"
	"github.com/zopping/inventory-svc/models"
	"github.com/zopping/inventory-svc/stores"
)

var _ stores.Reservation = (*Store)(nil)

// Store implements stores.Reservation on top of ctx.SQL.
type Store struct{}

// New returns a reservation store.
func New() *Store { return &Store{} }

// txn is the part of a GoFr transaction this store uses.
type txn interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type row struct {
	id        int64
	variantID int64
	sellerID  int64
	quantity  int64
	status    string
}

// Create holds stock for every item, or for none.
func (s *Store) Create(ctx *gofr.Context, req *models.ReservationRequest) (res models.Reservation, err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return res, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, it := range req.Items {
		if err = reserveItem(ctx, tx, req, it); err != nil {
			return res, err
		}
	}

	if err = tx.Commit(); err != nil {
		return res, err
	}

	return s.Get(ctx, req.OrderRef)
}

func reserveItem(ctx *gofr.Context, tx txn, req *models.ReservationRequest, it models.ReservationItem) error {
	var id int64

	err := tx.QueryRowContext(ctx,
		`INSERT INTO stock_reservations (order_ref, variant_id, seller_id, quantity, expires_at)
		 VALUES ($1, $2, $3, $4, now() + make_interval(secs => $5))
		 ON CONFLICT (order_ref, variant_id, seller_id) DO NOTHING RETURNING id`,
		req.OrderRef, it.VariantID, it.SellerID, it.Quantity, req.TTLSeconds).Scan(&id)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return checkRepeat(ctx, tx, req.OrderRef, it) // already reserved: a retry must match exactly
	case errors.Is(pgerr.Map(err), apperr.ErrInvalid):
		// this seller has no stock row for the variant at all
		return &apperr.InsufficientStock{VariantID: it.VariantID, SellerID: it.SellerID, Requested: it.Quantity}
	case err != nil:
		return pgerr.Map(err)
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE stock
		    SET reserved = CASE WHEN unlimited_stock THEN reserved ELSE reserved + $2 END, updated_at = now()
		  WHERE variant_id = $1 AND seller_id = $3 AND status = 'ENABLED' AND (unlimited_stock OR quantity - reserved >= $2)`,
		it.VariantID, it.Quantity, it.SellerID)
	if err != nil {
		return pgerr.Map(err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		var available int64

		_ = tx.QueryRowContext(ctx,
			`SELECT CASE WHEN status = 'ENABLED' THEN quantity - reserved ELSE 0 END FROM stock
			  WHERE variant_id = $1 AND seller_id = $2`, it.VariantID, it.SellerID).Scan(&available)

		return &apperr.InsufficientStock{VariantID: it.VariantID, SellerID: it.SellerID, Requested: it.Quantity, Available: available}
	}

	return nil
}

// checkRepeat accepts a repeated reservation only if it matches the existing one and is still live.
func checkRepeat(ctx *gofr.Context, tx txn, orderRef string, it models.ReservationItem) error {
	var (
		quantity int64
		status   string
	)

	if err := tx.QueryRowContext(ctx,
		`SELECT quantity, status FROM stock_reservations WHERE order_ref = $1 AND variant_id = $2 AND seller_id = $3`,
		orderRef, it.VariantID, it.SellerID).Scan(&quantity, &status); err != nil {
		return pgerr.Map(err)
	}

	if status == models.ReservationReleased {
		return &apperr.StateConflict{Reason: fmt.Sprintf("reservation for order %s was released; use a new orderRef", orderRef)}
	}

	if quantity != it.Quantity {
		return &apperr.StateConflict{Reason: fmt.Sprintf(
			"order %s already reserves %d of variant %d from seller %d, not %d", orderRef, quantity, it.VariantID, it.SellerID, it.Quantity)}
	}

	return nil
}

// Get returns a reservation by order reference.
func (*Store) Get(ctx *gofr.Context, orderRef string) (models.Reservation, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT variant_id, seller_id, quantity, status, expires_at, created_at FROM stock_reservations
		  WHERE order_ref = $1 ORDER BY variant_id, seller_id`, orderRef)
	if err != nil {
		return models.Reservation{}, pgerr.Map(err)
	}
	defer rows.Close()

	res := models.Reservation{OrderRef: orderRef, Items: []models.ReservationItem{}}
	statuses := map[string]bool{}

	for rows.Next() {
		var (
			it                 models.ReservationItem
			status             string
			expiresAt, created time.Time
		)

		if err := rows.Scan(&it.VariantID, &it.SellerID, &it.Quantity, &status, &expiresAt, &created); err != nil {
			return models.Reservation{}, err
		}

		res.Items = append(res.Items, it)
		statuses[status] = true

		if expiresAt.After(res.ExpiresAt) {
			res.ExpiresAt = expiresAt
		}

		if res.CreatedAt.IsZero() || created.Before(res.CreatedAt) {
			res.CreatedAt = created
		}
	}

	if err := rows.Err(); err != nil {
		return models.Reservation{}, err
	}

	if len(res.Items) == 0 {
		return models.Reservation{}, apperr.ErrNotFound
	}

	res.Status = overall(statuses)

	return res, nil
}

// overall is RESERVED while anything is still held, else CONFIRMED if anything was sold, else RELEASED.
func overall(statuses map[string]bool) string {
	switch {
	case statuses[models.ReservationReserved]:
		return models.ReservationReserved
	case statuses[models.ReservationConfirmed]:
		return models.ReservationConfirmed
	default:
		return models.ReservationReleased
	}
}

// lock reads and locks every reservation row of an order, in variant order.
func lock(tx txn, orderRef string) ([]row, error) {
	rows, err := tx.Query(
		`SELECT id, variant_id, seller_id, quantity, status FROM stock_reservations WHERE order_ref = $1
		  ORDER BY variant_id, seller_id FOR UPDATE`,
		orderRef)
	if err != nil {
		return nil, pgerr.Map(err)
	}
	defer rows.Close()

	var out []row

	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.variantID, &r.sellerID, &r.quantity, &r.status); err != nil {
			return nil, err
		}

		out = append(out, r)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(out) == 0 {
		return nil, apperr.ErrNotFound
	}

	return out, nil
}

// Confirm turns every held item into a sale. Already-confirmed items are skipped, so a retry is safe.
func (s *Store) Confirm(ctx *gofr.Context, orderRef string) (models.Reservation, error) {
	return s.settle(ctx, orderRef, models.ReservationConfirmed)
}

// Release gives every held item back. Already-released items are skipped, so a retry is safe.
func (s *Store) Release(ctx *gofr.Context, orderRef string) (models.Reservation, error) {
	return s.settle(ctx, orderRef, models.ReservationReleased)
}

func (s *Store) settle(ctx *gofr.Context, orderRef, target string) (res models.Reservation, err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return res, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	rows, err := lock(tx, orderRef)
	if err != nil {
		return res, err
	}

	for _, r := range rows {
		if r.status == target {
			continue
		}

		if r.status != models.ReservationReserved {
			err = &apperr.StateConflict{Reason: fmt.Sprintf(
				"order %s is %s and cannot be %s", orderRef, lowerStatus(r.status), lowerStatus(target))}

			return res, err
		}

		if err = apply(ctx, tx, orderRef, r, target); err != nil {
			return res, err
		}
	}

	if err = tx.Commit(); err != nil {
		return res, err
	}

	return s.Get(ctx, orderRef)
}

func lowerStatus(s string) string {
	switch s {
	case models.ReservationConfirmed:
		return "confirmed"
	case models.ReservationReleased:
		return "released"
	default:
		return "reserved"
	}
}

// apply moves one held item to CONFIRMED (stock leaves) or RELEASED (stock returns to available).
func apply(ctx *gofr.Context, tx txn, orderRef string, r row, target string) error {
	confirm := target == models.ReservationConfirmed

	var (
		quantity  int64
		unlimited bool
	)

	err := tx.QueryRowContext(ctx,
		`UPDATE stock
		    SET quantity = CASE WHEN unlimited_stock OR NOT $3 THEN quantity ELSE quantity - $2 END,
		        reserved = CASE WHEN unlimited_stock THEN reserved ELSE reserved - $2 END,
		        updated_at = now()
		  WHERE variant_id = $1 AND seller_id = $4
		 RETURNING quantity, unlimited_stock`, r.variantID, r.quantity, confirm, r.sellerID).Scan(&quantity, &unlimited)
	if err != nil {
		return pgerr.Map(err)
	}

	if confirm && !unlimited {
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO stock_movements (variant_id, seller_id, delta, quantity_after, reason, reference_type, reference_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			r.variantID, r.sellerID, -r.quantity, quantity, models.ReasonSold, models.ReferenceOrder, orderRef); err != nil {
			return pgerr.Map(err)
		}
	}

	_, err = tx.ExecContext(ctx, `UPDATE stock_reservations SET status = $2, updated_at = now() WHERE id = $1`, r.id, target)

	return pgerr.Map(err)
}

// ExpiredOrderRefs lists orders whose hold has run past its expiry.
func (*Store) ExpiredOrderRefs(ctx *gofr.Context, limit int) ([]string, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT DISTINCT order_ref FROM stock_reservations WHERE status = 'RESERVED' AND expires_at < now() LIMIT $1`, limit)
	if err != nil {
		return nil, pgerr.Map(err)
	}
	defer rows.Close()

	refs := []string{}

	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}

		refs = append(refs, ref)
	}

	return refs, rows.Err()
}
