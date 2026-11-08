// Package brand is the Postgres store for brands.
package brand

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/apperr"
	"github.com/zopping/catalogue-svc/common/pgerr"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/stores"
)

const columns = `id, name, slug, logo, image, video, description, tag_line, status, client_id, created_at, updated_at`

var _ stores.Brand = (*Store)(nil)

// Store implements stores.Brand on top of ctx.SQL.
type Store struct{}

// New returns a brand store.
func New() *Store { return &Store{} }

func scan(row interface{ Scan(...any) error }) (models.Brand, error) {
	var b models.Brand

	err := row.Scan(&b.ID, &b.Name, &b.Slug, &b.Logo, &b.Image, &b.Video, &b.Description, &b.TagLine,
		&b.Status, &b.ClientID, &b.CreatedAt, &b.UpdatedAt)

	return b, err
}

// Create inserts a brand; a duplicate slug yields apperr.ErrConflict.
func (*Store) Create(ctx *gofr.Context, req *models.BrandRequest) (models.Brand, error) {
	b, err := scan(ctx.SQL.QueryRowContext(ctx,
		`INSERT INTO brands (name, slug, logo, image, video, description, tag_line, status, client_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING `+columns,
		req.Name, req.Slug, req.Logo, req.Image, req.Video, req.Description, req.TagLine, req.Status, req.ClientID))

	return b, pgerr.Map(err)
}

// Get fetches one brand by id.
func (*Store) Get(ctx *gofr.Context, id int64) (models.Brand, error) {
	b, err := scan(ctx.SQL.QueryRowContext(ctx, `SELECT `+columns+` FROM brands WHERE id = $1 AND deleted_at IS NULL`, id))

	return b, pgerr.Map(err)
}

// List returns one page of brands plus the total number of matches.
func (*Store) List(ctx *gofr.Context, f *filters.BrandFilter) (brands []models.Brand, count int, err error) {
	where, args := f.Where()

	if err = ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM brands`+where, args...).Scan(&count); err != nil {
		return nil, 0, pgerr.Map(err)
	}

	paging, args := f.Page(args)

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+columns+` FROM brands`+where+paging, args...)
	if err != nil {
		return nil, 0, pgerr.Map(err)
	}
	defer rows.Close()

	brands = []models.Brand{}

	for rows.Next() {
		b, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}

		brands = append(brands, b)
	}

	return brands, count, rows.Err()
}

// Update replaces a brand's editable fields.
func (*Store) Update(ctx *gofr.Context, id int64, req *models.BrandRequest) (models.Brand, error) {
	b, err := scan(ctx.SQL.QueryRowContext(ctx,
		`UPDATE brands SET name = $2, slug = $3, logo = $4, image = $5, video = $6, description = $7, tag_line = $8,
		        status = $9, client_id = $10, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING `+columns,
		id, req.Name, req.Slug, req.Logo, req.Image, req.Video, req.Description, req.TagLine, req.Status, req.ClientID))

	return b, pgerr.Map(err)
}

// Delete soft-deletes a brand.
func (*Store) Delete(ctx *gofr.Context, id int64) error {
	res, err := ctx.SQL.ExecContext(ctx, `UPDATE brands SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return pgerr.Map(err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return apperr.ErrNotFound
	}

	return nil
}
