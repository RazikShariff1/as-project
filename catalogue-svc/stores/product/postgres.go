// Package product is the Postgres store for products and their category links.
package product

import (
	"context"
	"database/sql"

	"github.com/lib/pq"
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/apperr"
	"github.com/zopping/catalogue-svc/common/pgerr"
	"github.com/zopping/catalogue-svc/common/pgtypes"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/stores"
)

const columns = `id, primary_category_id, brand_id, name, slug, tag_line, description, images, videos, status,
	has_variants, sold_by_weight, meta_data, client_item_id, barcodes, created_at, updated_at`

var _ stores.Product = (*Store)(nil)

// Store implements stores.Product on top of ctx.SQL.
type Store struct{}

// New returns a product store.
func New() *Store { return &Store{} }

// execer is the part of ctx.SQL and *Tx that writes category links.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func optID(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}

	return &n.Int64
}

func scan(row interface{ Scan(...any) error }) (models.Product, error) {
	var (
		p                     models.Product
		category, brand       sql.NullInt64
		meta                  []byte
		images, videos, codes []string
	)

	err := row.Scan(&p.ID, &category, &brand, &p.Name, &p.Slug, &p.TagLine, &p.Description,
		pq.Array(&images), pq.Array(&videos), &p.Status, &p.HasVariants, &p.SoldByWeight, &meta, &p.ClientItemID,
		pq.Array(&codes), &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return p, err
	}

	p.PrimaryCategoryID, p.BrandID = optID(category), optID(brand)
	p.Images, p.Videos, p.Barcodes = pgtypes.NonNil(images), pgtypes.NonNil(videos), pgtypes.NonNil(codes)
	p.CategoryIDs, p.Variants = []int64{}, []models.Variant{}
	p.MetaData, err = pgtypes.DecodeJSON(meta)

	return p, err
}

// writeArgs are the editable columns shared by INSERT and UPDATE, in a fixed order.
func writeArgs(req *models.ProductRequest) ([]any, error) {
	meta, err := pgtypes.EncodeJSON(req.MetaData)
	if err != nil {
		return nil, err
	}

	return []any{
		req.PrimaryCategoryID, req.BrandID, req.Name, req.Slug, req.TagLine, req.Description,
		pgtypes.Strings(req.Images), pgtypes.Strings(req.Videos), req.Status, req.HasVariants, req.SoldByWeight,
		meta, req.ClientItemID, pgtypes.Strings(req.Barcodes),
	}, nil
}

// setCategories replaces the product's extra categories.
func setCategories(ctx context.Context, db execer, productID int64, ids []int64) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM category_product WHERE product_id = $1`, productID); err != nil {
		return err
	}

	if len(ids) == 0 {
		return nil
	}

	_, err := db.ExecContext(ctx,
		`INSERT INTO category_product (category_id, product_id) SELECT unnest($1::bigint[]), $2 ON CONFLICT DO NOTHING`,
		pgtypes.Int64s(ids), productID)

	return err
}

// fillCategories sets CategoryIDs on each product with one query.
func fillCategories(ctx *gofr.Context, products []models.Product) error {
	if len(products) == 0 {
		return nil
	}

	ids := make([]int64, len(products))
	index := make(map[int64]int, len(products))

	for i := range products {
		ids[i] = products[i].ID
		index[products[i].ID] = i
	}

	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT product_id, category_id FROM category_product WHERE product_id = ANY($1::bigint[]) ORDER BY category_id`,
		pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var productID, categoryID int64
		if err := rows.Scan(&productID, &categoryID); err != nil {
			return err
		}

		p := &products[index[productID]]
		p.CategoryIDs = append(p.CategoryIDs, categoryID)
	}

	return rows.Err()
}

// Create inserts a product and its category links in one transaction.
// A duplicate slug yields apperr.ErrConflict; an unknown category or brand yields apperr.InvalidRef.
func (*Store) Create(ctx *gofr.Context, req *models.ProductRequest) (p models.Product, err error) {
	args, err := writeArgs(req)
	if err != nil {
		return p, err
	}

	tx, err := ctx.SQL.Begin()
	if err != nil {
		return p, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	p, err = scan(tx.QueryRowContext(ctx,
		`INSERT INTO products (primary_category_id, brand_id, name, slug, tag_line, description, images, videos,
		        status, has_variants, sold_by_weight, meta_data, client_item_id, barcodes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 RETURNING `+columns, args...))
	if err != nil {
		return p, pgerr.Map(err)
	}

	if err = setCategories(ctx, tx, p.ID, req.CategoryIDs); err != nil {
		return p, pgerr.Map(err)
	}

	if err = tx.Commit(); err != nil {
		return p, err
	}

	p.CategoryIDs = pgtypes.NonNil(req.CategoryIDs)

	return p, nil
}

// Get fetches one product by id.
func (*Store) Get(ctx *gofr.Context, id int64) (models.Product, error) {
	p, err := scan(ctx.SQL.QueryRowContext(ctx,
		`SELECT `+columns+` FROM products WHERE id = $1 AND deleted_at IS NULL`, id))
	if err != nil {
		return p, pgerr.Map(err)
	}

	ps := []models.Product{p}
	if err := fillCategories(ctx, ps); err != nil {
		return p, err
	}

	return ps[0], nil
}

// List returns one page of products plus the total number of matches.
func (*Store) List(ctx *gofr.Context, f *filters.ProductFilter) (products []models.Product, count int, err error) {
	where, args := f.Where()

	if err = ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM products`+where, args...).Scan(&count); err != nil {
		return nil, 0, pgerr.Map(err)
	}

	paging, args := f.Page(args)

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+columns+` FROM products`+where+paging, args...)
	if err != nil {
		return nil, 0, pgerr.Map(err)
	}
	defer rows.Close()

	products = []models.Product{}

	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}

		products = append(products, p)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, err
	}

	return products, count, fillCategories(ctx, products)
}

// Update replaces a product's editable fields and category links in one transaction.
func (*Store) Update(ctx *gofr.Context, id int64, req *models.ProductRequest) (p models.Product, err error) {
	args, err := writeArgs(req)
	if err != nil {
		return p, err
	}

	tx, err := ctx.SQL.Begin()
	if err != nil {
		return p, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	p, err = scan(tx.QueryRowContext(ctx,
		`UPDATE products SET primary_category_id = $2, brand_id = $3, name = $4, slug = $5, tag_line = $6, description = $7,
		        images = $8, videos = $9, status = $10, has_variants = $11, sold_by_weight = $12,
		        meta_data = $13, client_item_id = $14, barcodes = $15, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING `+columns, append([]any{id}, args...)...))
	if err != nil {
		return p, pgerr.Map(err)
	}

	if err = setCategories(ctx, tx, p.ID, req.CategoryIDs); err != nil {
		return p, pgerr.Map(err)
	}

	if err = tx.Commit(); err != nil {
		return p, err
	}

	p.CategoryIDs = pgtypes.NonNil(req.CategoryIDs)

	return p, nil
}

// Delete soft-deletes a product and its variants in one transaction.
func (*Store) Delete(ctx *gofr.Context, id int64) (err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx, `UPDATE products SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return pgerr.Map(err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		err = apperr.ErrNotFound

		return err
	}

	if _, err = tx.ExecContext(ctx,
		`UPDATE variants SET deleted_at = now() WHERE product_id = $1 AND deleted_at IS NULL`, id); err != nil {
		return pgerr.Map(err)
	}

	return tx.Commit()
}
