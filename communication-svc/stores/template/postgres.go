// Package template is the Postgres store for email templates.
package template

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/communication-svc/common/apperr"
	"github.com/zopping/communication-svc/common/pgerr"
	"github.com/zopping/communication-svc/filters"
	"github.com/zopping/communication-svc/models"
	"github.com/zopping/communication-svc/stores"
)

const columns = `id, name, language, subject, body, status, created_at, updated_at`

var _ stores.Template = (*Store)(nil)

// Store implements stores.Template on top of ctx.SQL.
type Store struct{}

// New returns a template store.
func New() *Store { return &Store{} }

func scan(row interface{ Scan(...any) error }) (models.Template, error) {
	var t models.Template

	err := row.Scan(&t.ID, &t.Name, &t.Language, &t.Subject, &t.Body, &t.Status, &t.CreatedAt, &t.UpdatedAt)

	return t, err
}

// Find fetches the template for a name and language.
func (*Store) Find(ctx *gofr.Context, name, language string) (models.Template, error) {
	t, err := scan(ctx.SQL.QueryRowContext(ctx, `SELECT `+columns+` FROM templates WHERE name = $1 AND language = $2`, name, language))

	return t, pgerr.Map(err)
}

// Get fetches one template by id.
func (*Store) Get(ctx *gofr.Context, id int64) (models.Template, error) {
	t, err := scan(ctx.SQL.QueryRowContext(ctx, `SELECT `+columns+` FROM templates WHERE id = $1`, id))

	return t, pgerr.Map(err)
}

// List returns one page of templates plus the total number of matches.
func (*Store) List(ctx *gofr.Context, f *filters.TemplateFilter) (templates []models.Template, count int, err error) {
	where, args := f.Where()

	if err = ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM templates`+where, args...).Scan(&count); err != nil {
		return nil, 0, pgerr.Map(err)
	}

	paging, args := f.Page(args)

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+columns+` FROM templates`+where+` ORDER BY name, language`+paging, args...)
	if err != nil {
		return nil, 0, pgerr.Map(err)
	}
	defer rows.Close()

	templates = []models.Template{}

	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}

		templates = append(templates, t)
	}

	return templates, count, rows.Err()
}

// Create stores a template; a second one for the same name and language is a conflict.
func (*Store) Create(ctx *gofr.Context, req *models.TemplateRequest) (models.Template, error) {
	t, err := scan(ctx.SQL.QueryRowContext(ctx,
		`INSERT INTO templates (name, language, subject, body, status) VALUES ($1, $2, $3, $4, $5) RETURNING `+columns,
		req.Name, req.Language, req.Subject, req.Body, req.Status))

	return t, pgerr.Map(err)
}

// Update replaces a template.
func (*Store) Update(ctx *gofr.Context, id int64, req *models.TemplateRequest) (models.Template, error) {
	t, err := scan(ctx.SQL.QueryRowContext(ctx,
		`UPDATE templates SET name = $2, language = $3, subject = $4, body = $5, status = $6, updated_at = now() WHERE id = $1 RETURNING `+columns,
		id, req.Name, req.Language, req.Subject, req.Body, req.Status))

	return t, pgerr.Map(err)
}

// Delete removes a template.
func (*Store) Delete(ctx *gofr.Context, id int64) error {
	res, err := ctx.SQL.ExecContext(ctx, `DELETE FROM templates WHERE id = $1`, id)
	if err != nil {
		return pgerr.Map(err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return apperr.ErrNotFound
	}

	return nil
}
