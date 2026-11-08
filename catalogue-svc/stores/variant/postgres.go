// Package variant is the Postgres store for variants.
package variant

import (
	"errors"

	"github.com/lib/pq"
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/apperr"
	"github.com/zopping/catalogue-svc/common/pgerr"
	"github.com/zopping/catalogue-svc/common/pgtypes"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/stores"
)

const columns = `id, product_id, client_item_id, name, status, barcodes, images, videos,
	meta_data, created_at, updated_at`

var _ stores.Variant = (*Store)(nil)

// Store implements stores.Variant on top of ctx.SQL.
type Store struct{}

// New returns a variant store.
func New() *Store { return &Store{} }

func scan(row interface{ Scan(...any) error }) (models.Variant, error) {
	var (
		v                     models.Variant
		meta                  []byte
		codes, images, videos []string
	)

	err := row.Scan(&v.ID, &v.ProductID, &v.ClientItemID, &v.Name, &v.Status,
		pq.Array(&codes), pq.Array(&images), pq.Array(&videos), &meta, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return v, err
	}

	v.Barcodes, v.Images, v.Videos = pgtypes.NonNil(codes), pgtypes.NonNil(images), pgtypes.NonNil(videos)
	v.MetaData, err = pgtypes.DecodeJSON(meta)

	return v, err
}

func collect(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]models.Variant, error) {
	out := []models.Variant{}

	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, v)
	}

	return out, rows.Err()
}

// Create inserts a variant under a live product. An unknown or deleted product yields apperr.InvalidRef,
// a duplicate (product, clientItemId) apperr.ErrConflict.
func (*Store) Create(ctx *gofr.Context, req *models.VariantRequest) (models.Variant, error) {
	meta, err := pgtypes.EncodeJSON(req.MetaData)
	if err != nil {
		return models.Variant{}, err
	}

	v, err := scan(ctx.SQL.QueryRowContext(ctx,
		`INSERT INTO variants (product_id, client_item_id, name, status,
		        barcodes, images, videos, meta_data)
		 SELECT $1, $2, $3, $4, $5, $6, $7, $8
		  WHERE EXISTS (SELECT 1 FROM products WHERE id = $1 AND deleted_at IS NULL)
		 RETURNING `+columns,
		req.ProductID, req.ClientItemID, req.Name, req.Status,
		pgtypes.Strings(req.Barcodes), pgtypes.Strings(req.Images), pgtypes.Strings(req.Videos), meta))

	err = pgerr.Map(err)
	if errors.Is(err, apperr.ErrNotFound) {
		return v, &apperr.InvalidRef{Constraint: "variants_product_id_fkey"}
	}

	return v, err
}

// Get fetches one variant by id.
func (*Store) Get(ctx *gofr.Context, id int64) (models.Variant, error) {
	v, err := scan(ctx.SQL.QueryRowContext(ctx,
		`SELECT `+columns+` FROM variants WHERE id = $1 AND deleted_at IS NULL`, id))

	return v, pgerr.Map(err)
}

// List returns one page of variants plus the total number of matches.
func (*Store) List(ctx *gofr.Context, f *filters.VariantFilter) (variants []models.Variant, count int, err error) {
	where, args := f.Where()

	if err = ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM variants`+where, args...).Scan(&count); err != nil {
		return nil, 0, pgerr.Map(err)
	}

	paging, args := f.Page(args)

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+columns+` FROM variants`+where+paging, args...)
	if err != nil {
		return nil, 0, pgerr.Map(err)
	}
	defer rows.Close()

	variants, err = collect(rows)

	return variants, count, err
}

// ListByProductIDs returns every live variant of the given products.
func (*Store) ListByProductIDs(ctx *gofr.Context, productIDs []int64) ([]models.Variant, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT `+columns+` FROM variants WHERE product_id = ANY($1::bigint[]) AND deleted_at IS NULL
		 ORDER BY created_at, id`, pq.Array(productIDs))
	if err != nil {
		return nil, pgerr.Map(err)
	}
	defer rows.Close()

	return collect(rows)
}

// Update replaces a variant's editable fields. The product never changes.
func (*Store) Update(ctx *gofr.Context, id int64, req *models.VariantRequest) (models.Variant, error) {
	meta, err := pgtypes.EncodeJSON(req.MetaData)
	if err != nil {
		return models.Variant{}, err
	}

	v, err := scan(ctx.SQL.QueryRowContext(ctx,
		`UPDATE variants SET client_item_id = $2, name = $3, status = $4,
		        barcodes = $5, images = $6, videos = $7, meta_data = $8, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING `+columns,
		id, req.ClientItemID, req.Name, req.Status,
		pgtypes.Strings(req.Barcodes), pgtypes.Strings(req.Images), pgtypes.Strings(req.Videos), meta))

	return v, pgerr.Map(err)
}

// Delete soft-deletes a variant.
func (*Store) Delete(ctx *gofr.Context, id int64) error {
	res, err := ctx.SQL.ExecContext(ctx, `UPDATE variants SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return pgerr.Map(err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return apperr.ErrNotFound
	}

	return nil
}
