// Package sellerrequest is the Postgres store for seller product requests and their items.
package sellerrequest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/apperr"
	"github.com/zopping/catalogue-svc/common/pgerr"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/stores"
)

const columns = `id, seller_id, product_id, status, rejection_reason, product_status, synced_at, created_at, updated_at`

var _ stores.SellerRequest = (*Store)(nil)

// Store implements stores.SellerRequest on top of ctx.SQL.
type Store struct{}

// New returns a seller request store.
func New() *Store { return &Store{} }

// txn is the part of a transaction this store uses.
type txn interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func scan(row interface{ Scan(...any) error }) (models.SellerProductRequest, error) {
	var (
		r      models.SellerProductRequest
		synced sql.NullTime
	)

	err := row.Scan(&r.ID, &r.SellerID, &r.ProductID, &r.Status, &r.RejectionReason, &r.ProductStatus, &synced, &r.CreatedAt, &r.UpdatedAt)
	r.Items = []models.SellerProductItem{}

	if synced.Valid {
		r.SyncedAt = &synced.Time
	}

	r.Synced = inSync(r.Status, synced, r.UpdatedAt)

	return r, err
}

// inSync reports whether the request's live state has reached inventory. Only APPROVED and ARCHIVED requests have anything
// live to apply; for the others there is nothing to wait for. A sync stamp older than the last change means it is stale.
func inSync(status string, synced sql.NullTime, updatedAt time.Time) bool {
	if status != models.RequestApproved && status != models.RequestArchived {
		return true
	}

	return synced.Valid && !synced.Time.Before(updatedAt)
}

// setItems replaces the request's items.
func setItems(ctx context.Context, db txn, requestID int64, items []models.SellerProductItem) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM seller_product_items WHERE request_id = $1`, requestID); err != nil {
		return err
	}

	if len(items) == 0 {
		return nil
	}

	variants, mrps, discounts := make([]int64, len(items)), make([]float64, len(items)), make([]float64, len(items))

	for i, it := range items {
		variants[i], mrps[i], discounts[i] = it.VariantID, it.MRP, it.Discount
	}

	_, err := db.ExecContext(ctx,
		`INSERT INTO seller_product_items (request_id, variant_id, mrp, discount)
		 SELECT $1, v, m, d FROM unnest($2::bigint[], $3::numeric[], $4::numeric[]) AS t(v, m, d)`,
		requestID, pq.Array(variants), pq.Array(mrps), pq.Array(discounts))

	return err
}

// fillItems sets Items on each request with one query.
func fillItems(ctx *gofr.Context, reqs []models.SellerProductRequest) error {
	if len(reqs) == 0 {
		return nil
	}

	ids := make([]int64, len(reqs))
	index := make(map[int64]int, len(reqs))

	for i := range reqs {
		ids[i] = reqs[i].ID
		index[reqs[i].ID] = i
	}

	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT request_id, variant_id, mrp, discount FROM seller_product_items
		  WHERE request_id = ANY($1::bigint[]) ORDER BY request_id, variant_id`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			requestID int64
			it        models.SellerProductItem
		)

		if err := rows.Scan(&requestID, &it.VariantID, &it.MRP, &it.Discount); err != nil {
			return err
		}

		r := &reqs[index[requestID]]
		r.Items = append(r.Items, it)
	}

	return rows.Err()
}

// notFoundOrConflict explains why a guarded UPDATE matched no row: the request does not exist, or its status forbids
// the action.
func notFoundOrConflict(ctx context.Context, db txn, id int64, action string) error {
	var status string

	err := db.QueryRowContext(ctx, `SELECT status FROM seller_product_requests WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.ErrNotFound
	}

	if err != nil {
		return pgerr.Map(err)
	}

	return &apperr.StateConflict{Reason: fmt.Sprintf("a %s request cannot be %s", strings.ToLower(status), action)}
}

// Create inserts a request and its items in one transaction.
func (*Store) Create(ctx *gofr.Context, in *models.SellerProductRequestInput) (r models.SellerProductRequest, err error) {
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
		`INSERT INTO seller_product_requests (seller_id, product_id, status) VALUES ($1, $2, $3) RETURNING `+columns,
		in.SellerID, in.ProductID, in.Status))
	if err != nil {
		return r, pgerr.Map(err)
	}

	if err = setItems(ctx, tx, r.ID, in.Items); err != nil {
		return r, pgerr.Map(err)
	}

	if err = tx.Commit(); err != nil {
		return r, err
	}

	r.Items = in.Items

	return r, nil
}

// Get fetches one request with its items.
func (*Store) Get(ctx *gofr.Context, id int64) (models.SellerProductRequest, error) {
	r, err := scan(ctx.SQL.QueryRowContext(ctx,
		`SELECT `+columns+` FROM seller_product_requests WHERE id = $1 AND deleted_at IS NULL`, id))
	if err != nil {
		return r, pgerr.Map(err)
	}

	rs := []models.SellerProductRequest{r}
	if err := fillItems(ctx, rs); err != nil {
		return r, err
	}

	return rs[0], nil
}

// List returns one page of requests plus the total number of matches.
func (*Store) List(ctx *gofr.Context, f *filters.SellerProductRequestFilter) (requests []models.SellerProductRequest, count int, err error) {
	where, args := f.Where()

	if err = ctx.SQL.QueryRowContext(ctx, `SELECT count(*) FROM seller_product_requests`+where, args...).Scan(&count); err != nil {
		return nil, 0, pgerr.Map(err)
	}

	paging, args := f.Page(args)

	rows, err := ctx.SQL.QueryContext(ctx, `SELECT `+columns+` FROM seller_product_requests`+where+paging, args...)
	if err != nil {
		return nil, 0, pgerr.Map(err)
	}
	defer rows.Close()

	requests = []models.SellerProductRequest{}

	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}

		requests = append(requests, r)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, err
	}

	return requests, count, fillItems(ctx, requests)
}

// Update replaces the status and items of a DRAFT or REJECTED request, clearing any old rejection reason.
func (*Store) Update(ctx *gofr.Context, id int64, in *models.SellerProductRequestInput) (r models.SellerProductRequest, err error) {
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
		`UPDATE seller_product_requests SET status = $2, rejection_reason = '', updated_at = now()
		  WHERE id = $1 AND deleted_at IS NULL AND status IN ('DRAFT', 'REJECTED') RETURNING `+columns, id, in.Status))
	if errors.Is(err, sql.ErrNoRows) {
		err = notFoundOrConflict(ctx, tx, id, "edited")

		return r, err
	}

	if err != nil {
		return r, pgerr.Map(err)
	}

	if err = setItems(ctx, tx, id, in.Items); err != nil {
		return r, pgerr.Map(err)
	}

	if err = tx.Commit(); err != nil {
		return r, err
	}

	r.Items = in.Items

	return r, nil
}

// Delete soft-deletes a request that is not APPROVED.
func (*Store) Delete(ctx *gofr.Context, id int64) error {
	res, err := ctx.SQL.ExecContext(ctx,
		`UPDATE seller_product_requests SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL AND status <> 'APPROVED'`, id)
	if err != nil {
		return pgerr.Map(err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return notFoundOrConflict(ctx, ctx.SQL, id, "deleted")
	}

	return nil
}

// Approve makes the request the seller's APPROVED one for the product, archiving any other, in one transaction.
func (*Store) Approve(ctx *gofr.Context, id int64) (changed []int64, err error) {
	tx, err := ctx.SQL.Begin()
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var (
		sellerID, productID int64
		status              string
	)

	err = tx.QueryRowContext(ctx,
		`SELECT seller_id, product_id, status FROM seller_product_requests WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id).
		Scan(&sellerID, &productID, &status)
	if err != nil {
		return nil, pgerr.Map(err)
	}

	switch status {
	case models.RequestApproved:
		return []int64{id}, tx.Commit() // already live; returned so the caller can re-send the event
	case models.RequestPending, models.RequestRejected:
	default:
		err = &apperr.StateConflict{Reason: fmt.Sprintf("a %s request cannot be approved", strings.ToLower(status))}

		return nil, err
	}

	archived, err := tx.QueryContext(ctx, // the partial unique index needs the old approval gone before the new one is set
		`UPDATE seller_product_requests SET status = 'ARCHIVED', updated_at = now()
		  WHERE seller_id = $1 AND product_id = $2 AND status = 'APPROVED' AND deleted_at IS NULL AND id <> $3 RETURNING id`,
		sellerID, productID, id)
	if err != nil {
		return nil, pgerr.Map(err)
	}

	changed = []int64{}

	for archived.Next() {
		var oldID int64
		if err = archived.Scan(&oldID); err != nil {
			_ = archived.Close()

			return nil, err
		}

		changed = append(changed, oldID)
	}

	if err = archived.Err(); err != nil {
		_ = archived.Close()

		return nil, err
	}

	_ = archived.Close()

	if _, err = tx.ExecContext(ctx,
		`UPDATE seller_product_requests SET status = 'APPROVED', rejection_reason = '', updated_at = now() WHERE id = $1`, id); err != nil {
		return nil, pgerr.Map(err)
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return append(changed, id), nil
}

// Reject moves a PENDING request to REJECTED with a reason.
func (s *Store) Reject(ctx *gofr.Context, id int64, reason string) (models.SellerProductRequest, error) {
	r, err := scan(ctx.SQL.QueryRowContext(ctx,
		`UPDATE seller_product_requests SET status = 'REJECTED', rejection_reason = $2, updated_at = now()
		  WHERE id = $1 AND deleted_at IS NULL AND status = 'PENDING' RETURNING `+columns, id, reason))
	if errors.Is(err, sql.ErrNoRows) {
		return r, notFoundOrConflict(ctx, ctx.SQL, id, "rejected")
	}

	if err != nil {
		return r, pgerr.Map(err)
	}

	return s.withItems(ctx, r)
}

// SetVisibility shows or hides an APPROVED request's live offer.
func (s *Store) SetVisibility(ctx *gofr.Context, id int64, productStatus string) (models.SellerProductRequest, error) {
	r, err := scan(ctx.SQL.QueryRowContext(ctx,
		`UPDATE seller_product_requests SET product_status = $2, updated_at = now()
		  WHERE id = $1 AND deleted_at IS NULL AND status = 'APPROVED' RETURNING `+columns, id, productStatus))
	if errors.Is(err, sql.ErrNoRows) {
		return r, notFoundOrConflict(ctx, ctx.SQL, id, "shown or hidden")
	}

	if err != nil {
		return r, pgerr.Map(err)
	}

	return s.withItems(ctx, r)
}

func (*Store) withItems(ctx *gofr.Context, r models.SellerProductRequest) (models.SellerProductRequest, error) {
	rs := []models.SellerProductRequest{r}
	if err := fillItems(ctx, rs); err != nil {
		return r, err
	}

	return rs[0], nil
}

// ApprovedVariants lists the variants the seller currently has approved for the product.
func (*Store) ApprovedVariants(ctx *gofr.Context, sellerID, productID int64) ([]int64, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT i.variant_id FROM seller_product_requests r JOIN seller_product_items i ON i.request_id = r.id
		  WHERE r.seller_id = $1 AND r.product_id = $2 AND r.status = 'APPROVED' AND r.deleted_at IS NULL
		  ORDER BY i.variant_id`, sellerID, productID)
	if err != nil {
		return nil, pgerr.Map(err)
	}
	defer rows.Close()

	ids := []int64{}

	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// MarkSynced records that the request's live state was applied to inventory. It only does so if the request has not
// changed since it was read (updatedAt still matches), so a change made in the meantime stays unsynced for the next pass.
func (*Store) MarkSynced(ctx *gofr.Context, id int64, updatedAt time.Time) error {
	_, err := ctx.SQL.ExecContext(ctx,
		`UPDATE seller_product_requests SET synced_at = now() WHERE id = $1 AND updated_at = $2`, id, updatedAt)

	return pgerr.Map(err)
}

// Unsynced lists APPROVED and ARCHIVED requests whose live state has not been applied since they last changed.
func (*Store) Unsynced(ctx *gofr.Context, limit int) ([]int64, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		`SELECT id FROM seller_product_requests
		  WHERE deleted_at IS NULL AND status IN ('APPROVED', 'ARCHIVED') AND (synced_at IS NULL OR synced_at < updated_at)
		  ORDER BY id LIMIT $1`, limit)
	if err != nil {
		return nil, pgerr.Map(err)
	}
	defer rows.Close()

	ids := []int64{}

	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}
