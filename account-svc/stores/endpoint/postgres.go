// Package endpoint is the Postgres store for endpoints.
package endpoint

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/account-svc/common/apperr"
	"github.com/zopping/account-svc/common/pgerr"
	"github.com/zopping/account-svc/filters"
	"github.com/zopping/account-svc/models"
	"github.com/zopping/account-svc/stores"
)

const columns = `id, service, method, path, description, created_at, updated_at`

var _ stores.Endpoint = (*Store)(nil)

// Store implements stores.Endpoint on top of ctx.SQL.
type Store struct{}

// New returns an endpoint store.
func New() *Store { return &Store{} }

func scan(row interface{ Scan(...any) error }) (models.Endpoint, error) {
	var e models.Endpoint

	err := row.Scan(&e.ID, &e.Service, &e.Method, &e.Path, &e.Description, &e.CreatedAt, &e.UpdatedAt)

	return e, err
}

// Create inserts an endpoint; a duplicate (service, method, path) yields apperr.ErrConflict.
func (*Store) Create(ctx *gofr.Context, req *models.EndpointRequest) (models.Endpoint, error) {
	e, err := scan(ctx.SQL.QueryRowContext(ctx,
		`INSERT INTO endpoints (service, method, path, description) VALUES ($1, $2, $3, $4) RETURNING `+columns,
		req.Service, req.Method, req.Path, req.Description))

	return e, pgerr.Map(err)
}

// Get fetches one endpoint by id.
func (*Store) Get(ctx *gofr.Context, id int64) (models.Endpoint, error) {
	e, err := scan(ctx.SQL.QueryRowContext(ctx, `SELECT `+columns+` FROM endpoints WHERE id = $1 AND deleted_at IS NULL`, id))

	return e, pgerr.Map(err)
}

// List returns one page of endpoints plus the total number of matches.
func (*Store) List(ctx *gofr.Context, f *filters.EndpointFilter) (endpoints []models.Endpoint, count int, err error) {
	where, args := f.Where()

	if err = ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM endpoints`+where, args...).Scan(&count); err != nil {
		return nil, 0, pgerr.Map(err)
	}

	paging, args := f.Page(args)

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+columns+` FROM endpoints`+where+paging, args...)
	if err != nil {
		return nil, 0, pgerr.Map(err)
	}
	defer rows.Close()

	endpoints = []models.Endpoint{}

	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}

		endpoints = append(endpoints, e)
	}

	return endpoints, count, rows.Err()
}

// Update replaces an endpoint's fields.
func (*Store) Update(ctx *gofr.Context, id int64, req *models.EndpointRequest) (models.Endpoint, error) {
	e, err := scan(ctx.SQL.QueryRowContext(ctx,
		`UPDATE endpoints SET service = $2, method = $3, path = $4, description = $5, updated_at = now()
		  WHERE id = $1 AND deleted_at IS NULL RETURNING `+columns, id, req.Service, req.Method, req.Path, req.Description))

	return e, pgerr.Map(err)
}

// Delete soft-deletes an endpoint and removes its role links in one transaction.
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

	res, err := tx.ExecContext(ctx, `UPDATE endpoints SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return pgerr.Map(err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		err = apperr.ErrNotFound

		return err
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM endpoint_role WHERE endpoint_id = $1`, id); err != nil {
		return pgerr.Map(err)
	}

	return tx.Commit()
}
