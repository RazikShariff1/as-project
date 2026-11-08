// Package seller is the Postgres store for sellers.
package seller

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/pgerr"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/stores"
)

const columns = `id, name, is_house, status, created_at, updated_at`

var _ stores.Seller = (*Store)(nil)

// Store implements stores.Seller on top of ctx.SQL.
type Store struct{}

// New returns a seller store.
func New() *Store { return &Store{} }

func scan(row interface{ Scan(...any) error }) (models.Seller, error) {
	var s models.Seller

	err := row.Scan(&s.ID, &s.Name, &s.IsHouse, &s.Status, &s.CreatedAt, &s.UpdatedAt)

	return s, err
}

// Create inserts a seller; a second house seller yields apperr.ErrConflict.
func (*Store) Create(ctx *gofr.Context, req *models.SellerRequest) (models.Seller, error) {
	s, err := scan(ctx.SQL.QueryRowContext(ctx,
		`INSERT INTO sellers (name, is_house) VALUES ($1, $2) RETURNING `+columns, req.Name, req.IsHouse))

	return s, pgerr.Map(err)
}

// Get fetches one seller by id.
func (*Store) Get(ctx *gofr.Context, id int64) (models.Seller, error) {
	s, err := scan(ctx.SQL.QueryRowContext(ctx, `SELECT `+columns+` FROM sellers WHERE id = $1`, id))

	return s, pgerr.Map(err)
}

// List returns sellers matching the filter.
func (*Store) List(ctx *gofr.Context, f *filters.SellerFilter) ([]models.Seller, error) {
	clause, args := f.Clause()

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+columns+` FROM sellers`+clause, args...)
	if err != nil {
		return nil, pgerr.Map(err)
	}
	defer rows.Close()

	out := []models.Seller{}

	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, s)
	}

	return out, rows.Err()
}

// Update replaces a seller's name and status.
func (*Store) Update(ctx *gofr.Context, id int64, req *models.SellerRequest) (models.Seller, error) {
	s, err := scan(ctx.SQL.QueryRowContext(ctx,
		`UPDATE sellers SET name = $2, status = $3, updated_at = now() WHERE id = $1 RETURNING `+columns,
		id, req.Name, req.Status))

	return s, pgerr.Map(err)
}
