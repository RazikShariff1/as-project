// Package role is the Postgres store for roles and their endpoint links.
package role

import (
	"context"
	"database/sql"

	"github.com/lib/pq"
	"gofr.dev/pkg/gofr"

	"github.com/zopping/account-svc/common/apperr"
	"github.com/zopping/account-svc/common/pgerr"
	"github.com/zopping/account-svc/common/pgtypes"
	"github.com/zopping/account-svc/filters"
	"github.com/zopping/account-svc/models"
	"github.com/zopping/account-svc/stores"
)

const columns = `id, name, description, all_access, created_at, updated_at`

var _ stores.Role = (*Store)(nil)

// Store implements stores.Role on top of ctx.SQL.
type Store struct{}

// New returns a role store.
func New() *Store { return &Store{} }

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func scan(row interface{ Scan(...any) error }) (models.Role, error) {
	var r models.Role

	err := row.Scan(&r.ID, &r.Name, &r.Description, &r.AllAccess, &r.CreatedAt, &r.UpdatedAt)
	r.EndpointIDs = []int64{}

	return r, err
}

// setEndpoints replaces the role's endpoints. Every id must be a live endpoint.
func setEndpoints(ctx context.Context, db execer, roleID int64, endpointIDs []int64) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM endpoint_role WHERE role_id = $1`, roleID); err != nil {
		return err
	}

	if len(endpointIDs) == 0 {
		return nil
	}

	res, err := db.ExecContext(ctx,
		`INSERT INTO endpoint_role (endpoint_id, role_id)
		 SELECT id, $1 FROM endpoints WHERE id = ANY($2::bigint[]) AND deleted_at IS NULL`, roleID, pgtypes.Int64s(endpointIDs))
	if err != nil {
		return err
	}

	if n, _ := res.RowsAffected(); int(n) != len(endpointIDs) {
		return &apperr.InvalidRef{Constraint: "endpoint_role_endpoint_id_fkey"}
	}

	return nil
}

// fillEndpoints sets EndpointIDs on each role with one query, skipping deleted endpoints.
func fillEndpoints(ctx *gofr.Context, roles []models.Role) error {
	if len(roles) == 0 {
		return nil
	}

	ids := make([]int64, len(roles))
	index := make(map[int64]int, len(roles))

	for i := range roles {
		ids[i] = roles[i].ID
		index[roles[i].ID] = i
	}

	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT er.role_id, er.endpoint_id FROM endpoint_role er
		   JOIN endpoints e ON e.id = er.endpoint_id AND e.deleted_at IS NULL
		  WHERE er.role_id = ANY($1::bigint[]) ORDER BY er.endpoint_id`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var roleID, endpointID int64
		if err := rows.Scan(&roleID, &endpointID); err != nil {
			return err
		}

		r := &roles[index[roleID]]
		r.EndpointIDs = append(r.EndpointIDs, endpointID)
	}

	return rows.Err()
}

// Create inserts a role and its endpoint links in one transaction.
func (*Store) Create(ctx *gofr.Context, req *models.RoleRequest) (r models.Role, err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return r, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	r, err = scan(tx.QueryRowContext(ctx,
		`INSERT INTO roles (name, description, all_access) VALUES ($1, $2, $3) RETURNING `+columns,
		req.Name, req.Description, req.AllAccess))
	if err != nil {
		return r, pgerr.Map(err)
	}

	if err = setEndpoints(ctx, tx, r.ID, req.EndpointIDs); err != nil {
		return r, pgerr.Map(err)
	}

	if err = tx.Commit(); err != nil {
		return r, err
	}

	r.EndpointIDs = pgtypes.NonNil(req.EndpointIDs)

	return r, nil
}

// Get fetches one role by id.
func (*Store) Get(ctx *gofr.Context, id int64) (models.Role, error) {
	r, err := scan(ctx.SQL.QueryRowContext(ctx, `SELECT `+columns+` FROM roles WHERE id = $1 AND deleted_at IS NULL`, id))
	if err != nil {
		return r, pgerr.Map(err)
	}

	rs := []models.Role{r}
	if err := fillEndpoints(ctx, rs); err != nil {
		return r, err
	}

	return rs[0], nil
}

// List returns one page of roles plus the total number of matches.
func (*Store) List(ctx *gofr.Context, f *filters.RoleFilter) (roles []models.Role, count int, err error) {
	where, args := f.Where()

	if err = ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM roles`+where, args...).Scan(&count); err != nil {
		return nil, 0, pgerr.Map(err)
	}

	paging, args := f.Page(args)

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+columns+` FROM roles`+where+paging, args...)
	if err != nil {
		return nil, 0, pgerr.Map(err)
	}
	defer rows.Close()

	roles = []models.Role{}

	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}

		roles = append(roles, r)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, err
	}

	return roles, count, fillEndpoints(ctx, roles)
}

// Update replaces a role's fields and endpoint links in one transaction.
func (*Store) Update(ctx *gofr.Context, id int64, req *models.RoleRequest) (r models.Role, err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return r, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	r, err = scan(tx.QueryRowContext(ctx,
		`UPDATE roles SET name = $2, description = $3, all_access = $4, updated_at = now()
		  WHERE id = $1 AND deleted_at IS NULL RETURNING `+columns, id, req.Name, req.Description, req.AllAccess))
	if err != nil {
		return r, pgerr.Map(err)
	}

	if err = setEndpoints(ctx, tx, id, req.EndpointIDs); err != nil {
		return r, pgerr.Map(err)
	}

	if err = tx.Commit(); err != nil {
		return r, err
	}

	r.EndpointIDs = pgtypes.NonNil(req.EndpointIDs)

	return r, nil
}

// Delete soft-deletes a role and removes its user and endpoint links in one transaction.
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

	res, err := tx.ExecContext(ctx, `UPDATE roles SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return pgerr.Map(err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		err = apperr.ErrNotFound

		return err
	}

	for _, q := range []string{`DELETE FROM user_role WHERE role_id = $1`, `DELETE FROM endpoint_role WHERE role_id = $1`} {
		if _, err = tx.ExecContext(ctx, q, id); err != nil {
			return pgerr.Map(err)
		}
	}

	return tx.Commit()
}

// Permissions reports all-access and the path patterns granted for a service and method.
func (*Store) Permissions(ctx *gofr.Context, userID int64, service, method string) (allAccess bool, patterns []string, err error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT r.all_access, e.path
		   FROM user_role ur
		   JOIN roles r ON r.id = ur.role_id AND r.deleted_at IS NULL
		   LEFT JOIN endpoint_role er ON er.role_id = r.id
		   LEFT JOIN endpoints e ON e.id = er.endpoint_id AND e.deleted_at IS NULL AND e.service = $2 AND e.method = $3
		  WHERE ur.user_id = $1`, userID, service, method)
	if err != nil {
		return false, nil, pgerr.Map(err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	patterns = []string{}

	for rows.Next() {
		var (
			all  bool
			path sql.NullString
		)

		if err := rows.Scan(&all, &path); err != nil {
			return false, nil, err
		}

		allAccess = allAccess || all

		if path.Valid && !seen[path.String] {
			seen[path.String] = true

			patterns = append(patterns, path.String)
		}
	}

	return allAccess, patterns, rows.Err()
}
