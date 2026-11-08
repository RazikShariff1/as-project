// Package permissible is the Postgres store for the per-product whitelist of sellers.
package permissible

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/pgerr"
	"github.com/zopping/catalogue-svc/common/pgtypes"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/stores"
)

const columns = `seller_id, product_id, status, created_at, updated_at`

var _ stores.PermissibleSeller = (*Store)(nil)

// Store implements stores.PermissibleSeller on top of ctx.SQL.
type Store struct{}

// New returns a permissible-seller store.
func New() *Store { return &Store{} }

// Replace enables the listed sellers for the product and disables every other seller already listed for it.
func (*Store) Replace(ctx *gofr.Context, productID int64, sellerIDs []int64) (err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	ids := pgtypes.Int64s(sellerIDs)

	if _, err = tx.ExecContext(ctx,
		`UPDATE permissible_product_sellers SET status = 'DISABLED', updated_at = now()
		  WHERE product_id = $1 AND status <> 'DISABLED' AND seller_id <> ALL($2::bigint[])`, productID, ids); err != nil {
		return pgerr.Map(err)
	}

	if len(sellerIDs) > 0 {
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO permissible_product_sellers (seller_id, product_id, status)
			 SELECT unnest($2::bigint[]), $1, 'ENABLED'
			 ON CONFLICT (seller_id, product_id) DO UPDATE SET status = 'ENABLED', updated_at = now()`, productID, ids); err != nil {
			return pgerr.Map(err)
		}
	}

	return tx.Commit()
}

// Allowed reports whether the seller is ENABLED for the product.
func (*Store) Allowed(ctx *gofr.Context, sellerID, productID int64) (ok bool, err error) {
	err = ctx.SQL.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM permissible_product_sellers WHERE seller_id = $1 AND product_id = $2 AND status = 'ENABLED')`,
		sellerID, productID).Scan(&ok)

	return ok, pgerr.Map(err)
}

// List returns one page of entries plus the total number of matches.
func (*Store) List(ctx *gofr.Context, f *filters.PermissibleSellerFilter) (items []models.PermissibleSeller, count int, err error) {
	where, args := f.Where()

	if err = ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM permissible_product_sellers`+where, args...).Scan(&count); err != nil {
		return nil, 0, pgerr.Map(err)
	}

	paging, args := f.Page(args)

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+columns+` FROM permissible_product_sellers`+where+paging, args...)
	if err != nil {
		return nil, 0, pgerr.Map(err)
	}
	defer rows.Close()

	items = []models.PermissibleSeller{}

	for rows.Next() {
		var p models.PermissibleSeller
		if err := rows.Scan(&p.SellerID, &p.ProductID, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, 0, err
		}

		items = append(items, p)
	}

	return items, count, rows.Err()
}
