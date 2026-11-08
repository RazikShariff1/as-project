// Package sellerchange makes inventory-svc match the seller product requests. It is the subscriber side of the change
// event, which, as in catalogue-service, carries only request ids: this reads each request's current state, so handling an
// event twice, late, or out of order always ends at the same place.
package sellerchange

import (
	"errors"
	"fmt"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/clients/inventory"
	"github.com/zopping/catalogue-svc/common/apperr"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/services"
	"github.com/zopping/catalogue-svc/stores"
)

var _ services.SellerChange = (*Service)(nil)

// Service implements services.SellerChange.
type Service struct {
	requests  stores.SellerRequest
	inventory inventory.Client
}

// New returns a seller change service.
func New(requests stores.SellerRequest, inv inventory.Client) *Service {
	return &Service{requests: requests, inventory: inv}
}

// Process brings inventory in line with each request. The first inventory error stops the run and is returned, so the
// broker delivers the event again; everything already done is safe to repeat.
func (s *Service) Process(ctx *gofr.Context, requestIDs []int64) error {
	for _, id := range requestIDs {
		r, err := s.requests.Get(ctx, id)
		if errors.Is(err, apperr.ErrNotFound) {
			continue // deleted since the event was published; nothing live to change
		}

		if err != nil {
			return err
		}

		if err := s.apply(ctx, &r); err != nil {
			return err
		}

		// Only requests that have something live (APPROVED, or ARCHIVED and now withdrawn) are tracked as synced.
		if r.Status == models.RequestApproved || r.Status == models.RequestArchived {
			if err := s.requests.MarkSynced(ctx, r.ID, r.UpdatedAt); err != nil {
				return err
			}
		}
	}

	return nil
}

// Reconcile applies up to limit unsynced requests, one at a time, stopping at the first failure. It returns how many
// were applied, so a scheduler can see progress even when inventory is still down.
func (s *Service) Reconcile(ctx *gofr.Context, limit int) (int, error) {
	ids, err := s.requests.Unsynced(ctx, limit)
	if err != nil {
		return 0, err
	}

	applied := 0

	for _, id := range ids {
		if err := s.Process(ctx, []int64{id}); err != nil {
			return applied, err
		}

		applied++
	}

	return applied, nil
}

func (s *Service) apply(ctx *gofr.Context, r *models.SellerProductRequest) error {
	note := fmt.Sprintf("seller product request %d", r.ID)

	switch r.Status {
	case models.RequestApproved:
		for _, it := range r.Items {
			var err error

			if r.ProductStatus == models.StatusEnabled {
				err = s.inventory.UpsertOffer(ctx, it.VariantID, r.SellerID, it.MRP, it.Discount, note)
			} else {
				err = s.inventory.Withdraw(ctx, it.VariantID, r.SellerID)
			}

			if err != nil {
				return err
			}
		}
	case models.RequestArchived:
		// Replaced by a newer approval: withdraw only the variants the seller no longer has live, never one the new
		// approval still offers.
		live, err := s.requests.ApprovedVariants(ctx, r.SellerID, r.ProductID)
		if err != nil {
			return err
		}

		stillLive := make(map[int64]bool, len(live))
		for _, v := range live {
			stillLive[v] = true
		}

		for _, it := range r.Items {
			if stillLive[it.VariantID] {
				continue
			}

			if err := s.inventory.Withdraw(ctx, it.VariantID, r.SellerID); err != nil {
				return err
			}
		}
	default:
		// DRAFT, PENDING and REJECTED requests have nothing live.
	}

	return nil
}
