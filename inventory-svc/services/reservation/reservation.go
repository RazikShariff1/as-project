// Package reservation holds the business rules for stock reservations.
package reservation

import (
	"errors"
	"sort"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/inventory-svc/common/apperr"
	"github.com/zopping/inventory-svc/common/httpx"
	"github.com/zopping/inventory-svc/models"
	"github.com/zopping/inventory-svc/services"
	"github.com/zopping/inventory-svc/stores"
)

const (
	maxItems      = 100
	maxTTLSeconds = 24 * 60 * 60
)

var _ services.Reservation = (*Service)(nil)

// Service implements services.Reservation.
type Service struct {
	store stores.Reservation
}

// New returns a reservation service.
func New(store stores.Reservation) *Service { return &Service{store: store} }

// offerKey identifies one seller's offer for a variant.
type offerKey struct{ variantID, sellerID int64 }

// normalise validates the request, merges repeated offers, sorts items by variant then seller (so concurrent orders
// lock stock rows in the same order and cannot deadlock) and defaults the TTL.
func normalise(req *models.ReservationRequest) error {
	req.OrderRef = strings.TrimSpace(req.OrderRef)
	if req.OrderRef == "" {
		return httpx.InvalidParam("orderRef")
	}

	if len(req.Items) == 0 || len(req.Items) > maxItems {
		return httpx.InvalidParam("items")
	}

	merged := make(map[offerKey]int64, len(req.Items))

	for _, it := range req.Items {
		if it.VariantID < 1 || it.SellerID < 1 || it.Quantity < 1 {
			return httpx.InvalidParam("items")
		}

		merged[offerKey{it.VariantID, it.SellerID}] += it.Quantity
	}

	req.Items = req.Items[:0]
	for k, q := range merged {
		req.Items = append(req.Items, models.ReservationItem{VariantID: k.variantID, SellerID: k.sellerID, Quantity: q})
	}

	sort.Slice(req.Items, func(i, j int) bool {
		if req.Items[i].VariantID != req.Items[j].VariantID {
			return req.Items[i].VariantID < req.Items[j].VariantID
		}

		return req.Items[i].SellerID < req.Items[j].SellerID
	})

	if req.TTLSeconds == 0 {
		req.TTLSeconds = models.DefaultReservationTTLSeconds
	}

	if req.TTLSeconds < 1 || req.TTLSeconds > maxTTLSeconds {
		return httpx.InvalidParam("ttlSeconds")
	}

	return nil
}

func validRef(orderRef string) (string, error) {
	ref := strings.TrimSpace(orderRef)
	if ref == "" {
		return "", httpx.InvalidParam("orderRef")
	}

	return ref, nil
}

// Create validates and reserves stock for every item, or for none.
func (s *Service) Create(ctx *gofr.Context, req *models.ReservationRequest) (models.Reservation, error) {
	if err := normalise(req); err != nil {
		return models.Reservation{}, err
	}

	res, err := s.store.Create(ctx, req)
	if err != nil {
		return models.Reservation{}, httpx.FromStore(err, "orderRef", req.OrderRef)
	}

	return res, nil
}

// Get returns a reservation.
func (s *Service) Get(ctx *gofr.Context, orderRef string) (models.Reservation, error) {
	ref, err := validRef(orderRef)
	if err != nil {
		return models.Reservation{}, err
	}

	res, err := s.store.Get(ctx, ref)
	if err != nil {
		return models.Reservation{}, httpx.FromStore(err, "orderRef", ref)
	}

	return res, nil
}

// Confirm turns a reservation into a sale.
func (s *Service) Confirm(ctx *gofr.Context, orderRef string) (models.Reservation, error) {
	ref, err := validRef(orderRef)
	if err != nil {
		return models.Reservation{}, err
	}

	res, err := s.store.Confirm(ctx, ref)
	if err != nil {
		return models.Reservation{}, httpx.FromStore(err, "orderRef", ref)
	}

	return res, nil
}

// Release gives a reservation's stock back.
func (s *Service) Release(ctx *gofr.Context, orderRef string) (models.Reservation, error) {
	ref, err := validRef(orderRef)
	if err != nil {
		return models.Reservation{}, err
	}

	res, err := s.store.Release(ctx, ref)
	if err != nil {
		return models.Reservation{}, httpx.FromStore(err, "orderRef", ref)
	}

	return res, nil
}

// ReleaseExpired releases up to limit expired holds. An order that was confirmed or released in the meantime is
// not an error; it just isn't counted.
func (s *Service) ReleaseExpired(ctx *gofr.Context, limit int) (int, error) {
	refs, err := s.store.ExpiredOrderRefs(ctx, limit)
	if err != nil {
		return 0, httpx.FromStore(err, "limit", "")
	}

	released := 0

	for _, ref := range refs {
		_, err := s.store.Release(ctx, ref)

		var conflict *apperr.StateConflict

		switch {
		case err == nil:
			released++
		case errors.Is(err, apperr.ErrNotFound), errors.As(err, &conflict):
			continue
		default:
			return released, httpx.FromStore(err, "orderRef", ref)
		}
	}

	return released, nil
}
