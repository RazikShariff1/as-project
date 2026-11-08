package stock

import (
	"database/sql"
	"errors"
	"math"
	"strconv"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/inventory-svc/common/pgerr"
	"github.com/zopping/inventory-svc/filters"
	"github.com/zopping/inventory-svc/models"
)

const priceEpsilon = 1e-9

const insertPriceLog = `INSERT INTO stock_price_log (variant_id, seller_id, field, old_value, new_value, notes, user_id)
	VALUES ($1, $2, $3, $4, $5, $6, $7)`

// priceChange is one field that differs between the stored and the requested value.
type priceChange struct {
	field    string
	old, new *string
}

// SetPrice replaces the commercial details and logs every field that actually changed, in one transaction.
// A constraint failure (negative amount, discount above mrp) yields apperr.InvalidRef naming the constraint.
func (*Store) SetPrice(ctx *gofr.Context, variantID, sellerID int64, req *models.PriceRequest) (s models.Stock, err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return s, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var (
		oldMRP, oldCost    sql.NullFloat64
		oldDiscount        float64
		oldTax, oldHSNCode string
	)

	err = tx.QueryRowContext(ctx,
		`SELECT mrp, discount, cost_price, tax, hsn_code FROM stock WHERE variant_id = $1 AND seller_id = $2 FOR UPDATE`,
		variantID, sellerID).Scan(&oldMRP, &oldDiscount, &oldCost, &oldTax, &oldHSNCode)
	if err != nil {
		return s, pgerr.Map(err)
	}

	s, err = scan(tx.QueryRowContext(ctx,
		`UPDATE stock SET mrp = $3, discount = $4, cost_price = $5, tax = $6, hsn_code = $7, updated_at = now()
		  WHERE variant_id = $1 AND seller_id = $2 RETURNING `+columns,
		variantID, sellerID, req.MRP, req.Discount, req.CostPrice, req.Tax, req.HSNCode))
	if err != nil {
		return s, pgerr.Map(err)
	}

	changes := []priceChange{
		diffNullable(models.PriceFieldMRP, oldMRP, req.MRP),
		diffNumber(models.PriceFieldDiscount, oldDiscount, req.Discount),
		diffNullable(models.PriceFieldCostPrice, oldCost, req.CostPrice),
		diffText(models.PriceFieldTax, oldTax, req.Tax),
		diffText(models.PriceFieldHSNCode, oldHSNCode, req.HSNCode),
	}

	for _, c := range changes {
		if c.field == "" {
			continue
		}

		if _, err = tx.ExecContext(ctx, insertPriceLog, variantID, sellerID, c.field, c.old, c.new, req.Notes, req.UserID); err != nil {
			return s, pgerr.Map(err)
		}
	}

	return s, tx.Commit()
}

// UpsertOffer creates the seller's row with zero stock, or updates only its mrp and discount and makes it visible
// again. Quantity is never touched, so a redelivered approval cannot wipe stock.
func (*Store) UpsertOffer(ctx *gofr.Context, variantID, sellerID int64, req *models.OfferRequest) (s models.Stock, err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return s, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var (
		oldMRP      sql.NullFloat64
		oldDiscount float64
	)

	err = tx.QueryRowContext(ctx,
		`SELECT mrp, discount FROM stock WHERE variant_id = $1 AND seller_id = $2 FOR UPDATE`, variantID, sellerID).
		Scan(&oldMRP, &oldDiscount)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return s, pgerr.Map(err)
	}

	s, err = scan(tx.QueryRowContext(ctx,
		`INSERT INTO stock (variant_id, seller_id, mrp, discount) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (variant_id, seller_id)
		 DO UPDATE SET mrp = EXCLUDED.mrp, discount = EXCLUDED.discount, status = 'ENABLED', updated_at = now()
		 RETURNING `+columns, variantID, sellerID, req.MRP, req.Discount))
	if err != nil {
		return s, pgerr.Map(err)
	}

	newMRP := req.MRP
	changes := []priceChange{
		diffNullable(models.PriceFieldMRP, oldMRP, &newMRP),
		diffNumber(models.PriceFieldDiscount, oldDiscount, req.Discount),
	}

	for _, c := range changes {
		if c.field == "" {
			continue
		}

		if _, err = tx.ExecContext(ctx, insertPriceLog, variantID, sellerID, c.field, c.old, c.new, req.Notes, req.UserID); err != nil {
			return s, pgerr.Map(err)
		}
	}

	return s, tx.Commit()
}

func formatNumber(f float64) *string {
	v := strconv.FormatFloat(f, 'f', -1, 64)

	return &v
}

// diffNullable compares a nullable stored amount with a nullable requested one; a zero priceChange means unchanged.
func diffNullable(field string, old sql.NullFloat64, next *float64) priceChange {
	switch {
	case !old.Valid && next == nil:
		return priceChange{}
	case old.Valid && next != nil && math.Abs(old.Float64-*next) < priceEpsilon:
		return priceChange{}
	}

	c := priceChange{field: field}
	if old.Valid {
		c.old = formatNumber(old.Float64)
	}

	if next != nil {
		c.new = formatNumber(*next)
	}

	return c
}

func diffNumber(field string, old, next float64) priceChange {
	if math.Abs(old-next) < priceEpsilon {
		return priceChange{}
	}

	return priceChange{field: field, old: formatNumber(old), new: formatNumber(next)}
}

func diffText(field, old, next string) priceChange {
	if old == next {
		return priceChange{}
	}

	c := priceChange{field: field}
	if old != "" {
		c.old = &old
	}

	if next != "" {
		c.new = &next
	}

	return c
}

// PriceLog returns one page of an offer's price-change history, newest first.
func (*Store) PriceLog(ctx *gofr.Context, f *filters.PriceLogFilter) (changes []models.PriceChange, count int, err error) {
	where, args := f.Where()

	if err = ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM stock_price_log`+where, args...).Scan(&count); err != nil {
		return nil, 0, pgerr.Map(err)
	}

	paging, args := f.Page(args)

	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT id, variant_id, seller_id, field, old_value, new_value, notes, user_id, created_at FROM stock_price_log`+where+paging,
		args...)
	if err != nil {
		return nil, 0, pgerr.Map(err)
	}
	defer rows.Close()

	changes = []models.PriceChange{}

	for rows.Next() {
		var (
			c        models.PriceChange
			old, new sql.NullString
			user     sql.NullInt64
		)

		if err := rows.Scan(&c.ID, &c.VariantID, &c.SellerID, &c.Field, &old, &new, &c.Notes, &user, &c.CreatedAt); err != nil {
			return nil, 0, err
		}

		if old.Valid {
			c.OldValue = &old.String
		}

		if new.Valid {
			c.NewValue = &new.String
		}

		if user.Valid {
			c.UserID = &user.Int64
		}

		changes = append(changes, c)
	}

	return changes, count, rows.Err()
}
