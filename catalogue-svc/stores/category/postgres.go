// Package category is the Postgres store for categories.
package category

import (
	"database/sql"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/apperr"
	"github.com/zopping/catalogue-svc/common/pgerr"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/stores"
)

const columns = `id, parent_category_id, name, slug, image, video, description, tag_line, status, client_id, created_at, updated_at`

var _ stores.Category = (*Store)(nil)

// Store implements stores.Category on top of ctx.SQL.
type Store struct{}

// New returns a category store.
func New() *Store { return &Store{} }

func scan(row interface{ Scan(...any) error }) (models.Category, error) {
	var (
		c      models.Category
		parent sql.NullInt64
	)

	err := row.Scan(&c.ID, &parent, &c.Name, &c.Slug, &c.Image, &c.Video, &c.Description, &c.TagLine, &c.Status,
		&c.ClientID, &c.CreatedAt, &c.UpdatedAt)

	if parent.Valid {
		c.ParentCategoryID = &parent.Int64
	}

	return c, err
}

// Create inserts a category; a duplicate slug yields apperr.ErrConflict, an unknown parent apperr.ErrInvalid.
func (*Store) Create(ctx *gofr.Context, req *models.CategoryRequest) (models.Category, error) {
	c, err := scan(ctx.SQL.QueryRowContext(ctx,
		`INSERT INTO categories (parent_category_id, name, slug, image, video, description, tag_line, status, client_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING `+columns,
		req.ParentCategoryID, req.Name, req.Slug, req.Image, req.Video, req.Description, req.TagLine, req.Status, req.ClientID))

	return c, pgerr.Map(err)
}

// Get fetches one category by id.
func (*Store) Get(ctx *gofr.Context, id int64) (models.Category, error) {
	c, err := scan(ctx.SQL.QueryRowContext(ctx,
		`SELECT `+columns+` FROM categories WHERE id = $1 AND deleted_at IS NULL`, id))

	return c, pgerr.Map(err)
}

// List returns one page of categories plus the total number of matches.
func (*Store) List(ctx *gofr.Context, f *filters.CategoryFilter) (categories []models.Category, count int, err error) {
	where, args := f.Where()

	if err = ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM categories`+where, args...).Scan(&count); err != nil {
		return nil, 0, pgerr.Map(err)
	}

	paging, args := f.Page(args)

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+columns+` FROM categories`+where+paging, args...)
	if err != nil {
		return nil, 0, pgerr.Map(err)
	}
	defer rows.Close()

	categories = []models.Category{}

	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}

		categories = append(categories, c)
	}

	return categories, count, rows.Err()
}

// Update replaces a category's editable fields.
func (*Store) Update(ctx *gofr.Context, id int64, req *models.CategoryRequest) (models.Category, error) {
	c, err := scan(ctx.SQL.QueryRowContext(ctx,
		`UPDATE categories SET parent_category_id = $2, name = $3, slug = $4, image = $5, video = $6, description = $7,
		        tag_line = $8, status = $9, client_id = $10, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL RETURNING `+columns,
		id, req.ParentCategoryID, req.Name, req.Slug, req.Image, req.Video, req.Description, req.TagLine, req.Status, req.ClientID))

	return c, pgerr.Map(err)
}

// Delete soft-deletes a category.
func (*Store) Delete(ctx *gofr.Context, id int64) error {
	res, err := ctx.SQL.ExecContext(ctx, `UPDATE categories SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return pgerr.Map(err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return apperr.ErrNotFound
	}

	return nil
}
