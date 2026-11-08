package reservation

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/inventory-svc/common/apperr"
	"github.com/zopping/inventory-svc/models"
	"github.com/zopping/inventory-svc/stores"
)

func invalid(param string) error { return gofrhttp.ErrorInvalidParam{Params: []string{param}} }

func setup(t *testing.T) (*Service, *stores.MockReservation, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	st := stores.NewMockReservation(ctrl)

	return New(st), st, &gofr.Context{}
}

func item(variant, qty int64) models.ReservationItem {
	return models.ReservationItem{VariantID: variant, SellerID: 7, Quantity: qty}
}

func itemFrom(variant, seller, qty int64) models.ReservationItem {
	return models.ReservationItem{VariantID: variant, SellerID: seller, Quantity: qty}
}

func TestNormalise_Validation(t *testing.T) {
	tests := []struct {
		name string
		req  models.ReservationRequest
		want string
	}{
		{"blank order ref", models.ReservationRequest{OrderRef: "  ", Items: []models.ReservationItem{item(1, 1)}}, "orderRef"},
		{"no items", models.ReservationRequest{OrderRef: "O1"}, "items"},
		{"zero quantity", models.ReservationRequest{OrderRef: "O1", Items: []models.ReservationItem{item(1, 0)}}, "items"},
		{"bad variant", models.ReservationRequest{OrderRef: "O1", Items: []models.ReservationItem{item(0, 1)}}, "items"},
		{"no seller", models.ReservationRequest{OrderRef: "O1", Items: []models.ReservationItem{itemFrom(1, 0, 1)}}, "items"},
		{"negative ttl", models.ReservationRequest{OrderRef: "O1", Items: []models.ReservationItem{item(1, 1)}, TTLSeconds: -1}, "ttlSeconds"},
		{"huge ttl", models.ReservationRequest{OrderRef: "O1", Items: []models.ReservationItem{item(1, 1)}, TTLSeconds: 999999}, "ttlSeconds"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.req
			assert.Equal(t, invalid(tc.want), normalise(&req))
		})
	}

	tooMany := models.ReservationRequest{OrderRef: "O1"}
	for i := int64(1); i <= maxItems+1; i++ {
		tooMany.Items = append(tooMany.Items, item(i, 1))
	}

	assert.Equal(t, invalid("items"), normalise(&tooMany))
}

func TestNormalise_MergesSortsAndDefaultsTTL(t *testing.T) {
	req := &models.ReservationRequest{
		OrderRef: " O1 ",
		Items: []models.ReservationItem{
			itemFrom(9, 2, 1), itemFrom(3, 5, 2), itemFrom(9, 2, 4), itemFrom(9, 1, 1), itemFrom(3, 4, 1),
		},
	}

	assert.NoError(t, normalise(req))
	assert.Equal(t, "O1", req.OrderRef)
	assert.Equal(t, []models.ReservationItem{
		itemFrom(3, 4, 1), itemFrom(3, 5, 2), itemFrom(9, 1, 1), itemFrom(9, 2, 5),
	}, req.Items, "the same offer is merged; the same variant from two sellers stays two lines; sorted by variant then seller")
	assert.Equal(t, models.DefaultReservationTTLSeconds, req.TTLSeconds)
}

func TestCreate_ErrorMapping(t *testing.T) {
	svc, st, ctx := setup(t)
	req := func() *models.ReservationRequest {
		return &models.ReservationRequest{OrderRef: "O1", Items: []models.ReservationItem{item(1, 5)}}
	}

	st.EXPECT().Create(ctx, gomock.Any()).Return(models.Reservation{}, &apperr.InsufficientStock{VariantID: 1, SellerID: 7, Requested: 5, Available: 2})

	_, err := svc.Create(ctx, req())
	assert.EqualError(t, err, "insufficient stock for variant 1 from seller 7: requested 5, available 2")

	st.EXPECT().Create(ctx, gomock.Any()).Return(models.Reservation{}, &apperr.StateConflict{Reason: "released; use a new orderRef"})

	_, err = svc.Create(ctx, req())
	assert.EqualError(t, err, "released; use a new orderRef")

	st.EXPECT().Create(ctx, gomock.Any()).Return(models.Reservation{OrderRef: "O1", Status: models.ReservationReserved}, nil)

	got, err := svc.Create(ctx, req())
	assert.NoError(t, err)
	assert.Equal(t, models.ReservationReserved, got.Status)
}

func TestGetConfirmRelease(t *testing.T) {
	svc, st, ctx := setup(t)

	_, err := svc.Get(ctx, "  ")
	assert.Equal(t, invalid("orderRef"), err)

	st.EXPECT().Get(ctx, "NOPE").Return(models.Reservation{}, apperr.ErrNotFound)

	_, err = svc.Get(ctx, "NOPE")
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "orderRef", Value: "NOPE"}, err)

	st.EXPECT().Confirm(ctx, "O1").Return(models.Reservation{Status: models.ReservationConfirmed}, nil)

	got, err := svc.Confirm(ctx, " O1 ")
	assert.NoError(t, err)
	assert.Equal(t, models.ReservationConfirmed, got.Status)

	st.EXPECT().Release(ctx, "O1").Return(models.Reservation{}, &apperr.StateConflict{Reason: "order O1 is confirmed and cannot be released"})

	_, err = svc.Release(ctx, "O1")
	assert.EqualError(t, err, "order O1 is confirmed and cannot be released")
}

func TestReleaseExpired(t *testing.T) {
	svc, st, ctx := setup(t)

	st.EXPECT().ExpiredOrderRefs(ctx, 10).Return([]string{"A", "B", "C", "D"}, nil)
	st.EXPECT().Release(ctx, "A").Return(models.Reservation{}, nil)
	st.EXPECT().Release(ctx, "B").Return(models.Reservation{}, &apperr.StateConflict{Reason: "confirmed meanwhile"})
	st.EXPECT().Release(ctx, "C").Return(models.Reservation{}, apperr.ErrNotFound)
	st.EXPECT().Release(ctx, "D").Return(models.Reservation{}, nil)

	n, err := svc.ReleaseExpired(ctx, 10)
	assert.NoError(t, err)
	assert.Equal(t, 2, n, "orders confirmed or removed in the meantime are skipped, not counted, not errors")

	boom := errors.New("db down")
	st.EXPECT().ExpiredOrderRefs(ctx, 10).Return([]string{"A", "B"}, nil)
	st.EXPECT().Release(ctx, "A").Return(models.Reservation{}, nil)
	st.EXPECT().Release(ctx, "B").Return(models.Reservation{}, boom)

	n, err = svc.ReleaseExpired(ctx, 10)
	assert.ErrorIs(t, err, boom, "an unexpected error stops the sweep")
	assert.Equal(t, 1, n)
}
