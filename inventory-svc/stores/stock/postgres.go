// Package stock is the Postgres store for sellers' offers: stock levels, their movement log and prices.
package stock

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lib/pq"
	"gofr.dev/pkg/gofr"

	"github.com/zopping/inventory-svc/common/apperr"
	"github.com/zopping/inventory-svc/common/pgerr"
	"github.com/zopping/inventory-svc/filters"
	"github.com/zopping/inventory-svc/models"
	"github.com/zopping/inventory-svc/stores"
)

const (
	columns = `variant_id, seller_id, quantity, reserved, unlimited_stock, unit, status, low_stock_threshold,
		mrp, discount, cost_price, mrp - discount, tax, hsn_code, created_at, updated_at`

	movementColumns = `id, variant_id, seller_id, delta, quantity_after, reason, notes, user_id, reference_type, reference_id, created_at`

	insertMovement = `INSERT INTO stock_movements
		(variant_id, seller_id, delta, quantity_after, reason, notes, user_id, reference_type, reference_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
)

var _ stores.Stock = (*Store)(nil)

// Store implements stores.Stock on top of ctx.SQL.
type Store struct{}

// New returns a stock store.
func New() *Store { return &Store{} }

func scan(row interface{ Scan(...any) error }) (models.Stock, error) {
	var (
		s                     models.Stock
		low                   sql.NullInt64
		mrp, cost, sellingNum sql.NullFloat64
	)

	err := row.Scan(&s.VariantID, &s.SellerID, &s.Quantity, &s.Reserved, &s.UnlimitedStock, &s.Unit, &s.Status, &low,
		&mrp, &s.Discount, &cost, &sellingNum, &s.Tax, &s.HSNCode, &s.CreatedAt, &s.UpdatedAt)
	if low.Valid {
		s.LowStockThreshold = &low.Int64
	}

	s.MRP, s.CostPrice, s.SellingPrice = optFloat(mrp), optFloat(cost), optFloat(sellingNum)
	s.Available = s.Quantity - s.Reserved
	s.InStock = s.Status == models.StatusEnabled && (s.UnlimitedStock || s.Available > 0)

	return s, err
}

func optFloat(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}

	return &n.Float64
}

// Get fetches one seller's offer for a variant.
func (*Store) Get(ctx *gofr.Context, variantID, sellerID int64) (models.Stock, error) {
	s, err := scan(ctx.SQL.QueryRowContext(ctx,
		`SELECT `+columns+` FROM stock WHERE variant_id = $1 AND seller_id = $2`, variantID, sellerID))

	return s, pgerr.Map(err)
}

// List returns one page of stock rows plus the total number of matches.
func (*Store) List(ctx *gofr.Context, f *filters.StockFilter) (stock []models.Stock, count int, err error) {
	where, args := f.Where()

	if err = ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM stock`+where, args...).Scan(&count); err != nil {
		return nil, 0, pgerr.Map(err)
	}

	paging, args := f.Page(args)

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+columns+` FROM stock`+where+paging, args...)
	if err != nil {
		return nil, 0, pgerr.Map(err)
	}
	defer rows.Close()

	stock = []models.Stock{}

	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}

		stock = append(stock, s)
	}

	return stock, count, rows.Err()
}

// Put sets the absolute quantity, unit, unlimited flag and low-stock threshold of an offer that already exists, in one
// transaction with its RECOUNTED movement. It never creates a row and never changes status: offers are created, shown and
// hidden only through the catalogue's approval flow, so a seller cannot stock or revive a variant they were not approved for.
// A seller with no row for the variant gets apperr.ErrNotFound; a quantity below what is reserved yields apperr.InvalidRef
// (stock_reserved_within_quantity).
func (*Store) Put(ctx *gofr.Context, variantID, sellerID int64, req *models.StockRequest) (s models.Stock, err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return s, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var oldQty int64

	err = tx.QueryRowContext(ctx, `SELECT quantity FROM stock WHERE variant_id = $1 AND seller_id = $2 FOR UPDATE`,
		variantID, sellerID).Scan(&oldQty)
	if err != nil {
		return s, pgerr.Map(err)
	}

	s, err = scan(tx.QueryRowContext(ctx,
		`UPDATE stock SET quantity = $3, unlimited_stock = $4, unit = $5, low_stock_threshold = $6, updated_at = now()
		  WHERE variant_id = $1 AND seller_id = $2 RETURNING `+columns,
		variantID, sellerID, req.Quantity, req.UnlimitedStock, req.Unit, req.LowStockThreshold))
	if err != nil {
		return s, pgerr.Map(err)
	}

	if delta := req.Quantity - oldQty; delta != 0 {
		if _, err = tx.ExecContext(ctx, insertMovement, variantID, sellerID, delta, req.Quantity, models.ReasonRecounted,
			req.Notes, req.UserID, "", ""); err != nil {
			return s, pgerr.Map(err)
		}
	}

	return s, tx.Commit()
}

// Adjust changes the quantity by a delta in one transaction with its movement. A repeated
// (referenceType, referenceId, reason) yields apperr.ErrConflict and changes nothing.
func (*Store) Adjust(ctx *gofr.Context, variantID, sellerID int64, req *models.AdjustRequest) (s models.Stock, err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return s, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	s, err = scan(tx.QueryRowContext(ctx,
		`UPDATE stock SET quantity = quantity + $3, updated_at = now()
		  WHERE variant_id = $1 AND seller_id = $2 AND quantity + $3 >= 0 AND (unlimited_stock OR quantity + $3 >= reserved)
		 RETURNING `+columns, variantID, sellerID, req.Delta))
	if errors.Is(err, sql.ErrNoRows) {
		err = insufficientOrMissing(ctx, tx, variantID, sellerID, -req.Delta)

		return s, err
	}

	if err != nil {
		return s, pgerr.Map(err)
	}

	if _, err = tx.ExecContext(ctx, insertMovement, variantID, sellerID, req.Delta, s.Quantity, req.Reason,
		req.Notes, req.UserID, req.ReferenceType, req.ReferenceID); err != nil {
		return s, pgerr.Map(err)
	}

	return s, tx.Commit()
}

// insufficientOrMissing explains why a guarded UPDATE matched no row.
func insufficientOrMissing(ctx *gofr.Context, tx interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}, variantID, sellerID, requested int64) error {
	var quantity, reserved int64

	err := tx.QueryRowContext(ctx, `SELECT quantity, reserved FROM stock WHERE variant_id = $1 AND seller_id = $2`,
		variantID, sellerID).Scan(&quantity, &reserved)
	if err != nil {
		return pgerr.Map(err)
	}

	return &apperr.InsufficientStock{VariantID: variantID, SellerID: sellerID, Requested: requested, Available: quantity - reserved}
}

// Withdraw hides the offer; stock, history and existing reservations are kept.
func (*Store) Withdraw(ctx *gofr.Context, variantID, sellerID int64) error {
	res, err := ctx.SQL.ExecContext(ctx,
		`UPDATE stock SET status = $3, updated_at = now() WHERE variant_id = $1 AND seller_id = $2`,
		variantID, sellerID, models.StatusHidden)
	if err != nil {
		return pgerr.Map(err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return apperr.ErrNotFound
	}

	return nil
}

// Offers returns the visible offers of each requested variant, one entry per distinct id in request order.
func (*Store) Offers(ctx *gofr.Context, variantIDs []int64) ([]models.VariantAvailability, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT variant_id, seller_id, quantity - reserved, unlimited_stock, unit, mrp, discount, mrp - discount
		   FROM stock WHERE status = 'ENABLED' AND variant_id = ANY($1::bigint[]) ORDER BY variant_id, seller_id`,
		pq.Array(variantIDs))
	if err != nil {
		return nil, pgerr.Map(err)
	}
	defer rows.Close()

	byVariant := make(map[int64][]models.Offer, len(variantIDs))

	for rows.Next() {
		var (
			variantID         int64
			o                 models.Offer
			mrp, sellingPrice sql.NullFloat64
		)

		if err := rows.Scan(&variantID, &o.SellerID, &o.Available, &o.UnlimitedStock, &o.Unit, &mrp, &o.Discount,
			&sellingPrice); err != nil {
			return nil, err
		}

		o.MRP, o.SellingPrice = optFloat(mrp), optFloat(sellingPrice)
		o.InStock = o.UnlimitedStock || o.Available > 0
		byVariant[variantID] = append(byVariant[variantID], o)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]models.VariantAvailability, 0, len(variantIDs))
	seen := make(map[int64]bool, len(variantIDs))

	for _, id := range variantIDs {
		if seen[id] {
			continue
		}

		seen[id] = true

		offers := byVariant[id]
		if offers == nil {
			offers = []models.Offer{}
		}

		out = append(out, models.VariantAvailability{VariantID: id, Offers: offers})
	}

	return out, nil
}

// Movements returns one page of an offer's movement history, newest first.
func (*Store) Movements(ctx *gofr.Context, f *filters.MovementFilter) (movements []models.Movement, count int, err error) {
	where, args := f.Where()

	if err = ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM stock_movements`+where, args...).Scan(&count); err != nil {
		return nil, 0, pgerr.Map(err)
	}

	paging, args := f.Page(args)

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+movementColumns+` FROM stock_movements`+where+paging, args...)
	if err != nil {
		return nil, 0, pgerr.Map(err)
	}
	defer rows.Close()

	movements = []models.Movement{}

	for rows.Next() {
		var (
			m    models.Movement
			user sql.NullInt64
		)

		if err := rows.Scan(&m.ID, &m.VariantID, &m.SellerID, &m.Delta, &m.QuantityAfter, &m.Reason, &m.Notes, &user,
			&m.ReferenceType, &m.ReferenceID, &m.CreatedAt); err != nil {
			return nil, 0, err
		}

		if user.Valid {
			m.UserID = &user.Int64
		}

		movements = append(movements, m)
	}

	return movements, count, rows.Err()
}
