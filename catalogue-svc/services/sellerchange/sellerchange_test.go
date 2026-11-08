package sellerchange

import (
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/clients/inventory"
	"github.com/zopping/catalogue-svc/common/apperr"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/stores"
)

type fixture struct {
	svc       *Service
	requests  *stores.MockSellerRequest
	inventory *inventory.MockClient
	ctx       *gofr.Context
}

func setup(t *testing.T) fixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	f := fixture{requests: stores.NewMockSellerRequest(ctrl), inventory: inventory.NewMockClient(ctrl), ctx: &gofr.Context{}}
	f.svc = New(f.requests, f.inventory)

	return f
}

func request(id int64, status, productStatus string, variants ...int64) models.SellerProductRequest {
	r := models.SellerProductRequest{ID: id, SellerID: 7, ProductID: 3, Status: status, ProductStatus: productStatus}
	for _, v := range variants {
		r.Items = append(r.Items, models.SellerProductItem{VariantID: v, MRP: 120, Discount: 20})
	}

	return r
}

func TestApprovedAndEnabledUpsertsEveryItem(t *testing.T) {
	f := setup(t)

	f.requests.EXPECT().Get(f.ctx, int64(1)).Return(request(1, models.RequestApproved, models.StatusEnabled, 10, 11), nil)
	f.inventory.EXPECT().UpsertOffer(f.ctx, int64(10), int64(7), 120.0, 20.0, "seller product request 1").Return(nil)
	f.inventory.EXPECT().UpsertOffer(f.ctx, int64(11), int64(7), 120.0, 20.0, "seller product request 1").Return(nil)
	f.requests.EXPECT().MarkSynced(f.ctx, int64(1), gomock.Any()).Return(nil)

	assert.NoError(t, f.svc.Process(f.ctx, []int64{1}))
}

func TestApprovedButHiddenWithdrawsEveryItem(t *testing.T) {
	f := setup(t)

	f.requests.EXPECT().Get(f.ctx, int64(1)).Return(request(1, models.RequestApproved, models.StatusHidden, 10, 11), nil)
	f.inventory.EXPECT().Withdraw(f.ctx, int64(10), int64(7)).Return(nil)
	f.inventory.EXPECT().Withdraw(f.ctx, int64(11), int64(7)).Return(nil)
	f.requests.EXPECT().MarkSynced(f.ctx, int64(1), gomock.Any()).Return(nil)

	assert.NoError(t, f.svc.Process(f.ctx, []int64{1}))
}

func TestArchivedWithdrawsOnlyVariantsTheSellerNoLongerOffers(t *testing.T) {
	f := setup(t)

	// the old request offered 10, 11 and 12; the new approval still offers 11
	f.requests.EXPECT().Get(f.ctx, int64(4)).Return(request(4, models.RequestArchived, models.StatusEnabled, 10, 11, 12), nil)
	f.requests.EXPECT().ApprovedVariants(f.ctx, int64(7), int64(3)).Return([]int64{11}, nil)
	f.inventory.EXPECT().Withdraw(f.ctx, int64(10), int64(7)).Return(nil)
	f.inventory.EXPECT().Withdraw(f.ctx, int64(12), int64(7)).Return(nil)
	f.requests.EXPECT().MarkSynced(f.ctx, int64(4), gomock.Any()).Return(nil)

	assert.NoError(t, f.svc.Process(f.ctx, []int64{4}), "variant 11 must stay live: the new approval still offers it")
}

func TestReplacementEventEndsInTheSameStateInEitherOrder(t *testing.T) {
	for _, order := range [][]int64{{4, 9}, {9, 4}} {
		f := setup(t)

		f.requests.EXPECT().Get(f.ctx, int64(4)).Return(request(4, models.RequestArchived, models.StatusEnabled, 10, 11), nil)
		f.requests.EXPECT().Get(f.ctx, int64(9)).Return(request(9, models.RequestApproved, models.StatusEnabled, 11, 12), nil)
		f.requests.EXPECT().ApprovedVariants(f.ctx, int64(7), int64(3)).Return([]int64{11, 12}, nil)

		f.inventory.EXPECT().Withdraw(f.ctx, int64(10), int64(7)).Return(nil)
		f.inventory.EXPECT().UpsertOffer(f.ctx, int64(11), int64(7), 120.0, 20.0, "seller product request 9").Return(nil)
		f.inventory.EXPECT().UpsertOffer(f.ctx, int64(12), int64(7), 120.0, 20.0, "seller product request 9").Return(nil)
		f.requests.EXPECT().MarkSynced(f.ctx, int64(4), gomock.Any()).Return(nil)
		f.requests.EXPECT().MarkSynced(f.ctx, int64(9), gomock.Any()).Return(nil)

		assert.NoError(t, f.svc.Process(f.ctx, order), "order %v: only variant 10 is withdrawn, 11 and 12 are live", order)
	}
}

func TestRequestsWithNothingLiveAreIgnored(t *testing.T) {
	f := setup(t)

	for i, status := range []string{models.RequestDraft, models.RequestPending, models.RequestRejected} {
		id := int64(i + 1)
		f.requests.EXPECT().Get(f.ctx, id).Return(request(id, status, models.StatusEnabled, 10), nil)
	}

	assert.NoError(t, f.svc.Process(f.ctx, []int64{1, 2, 3}), "no inventory calls: a mock would fail the test on any")
}

func TestDeletedRequestsAreSkipped(t *testing.T) {
	f := setup(t)

	f.requests.EXPECT().Get(f.ctx, int64(1)).Return(models.SellerProductRequest{}, apperr.ErrNotFound)
	f.requests.EXPECT().Get(f.ctx, int64(2)).Return(request(2, models.RequestApproved, models.StatusEnabled, 10), nil)
	f.inventory.EXPECT().UpsertOffer(f.ctx, int64(10), int64(7), 120.0, 20.0, "seller product request 2").Return(nil)
	f.requests.EXPECT().MarkSynced(f.ctx, int64(2), gomock.Any()).Return(nil)

	assert.NoError(t, f.svc.Process(f.ctx, []int64{1, 2}), "a missing request does not block the rest of the event")
}

func TestAnInventoryFailureIsReturnedSoTheEventIsRedelivered(t *testing.T) {
	f := setup(t)
	boom := errors.New("inventory-svc unreachable")

	f.requests.EXPECT().Get(f.ctx, int64(1)).Return(request(1, models.RequestApproved, models.StatusEnabled, 10, 11), nil)
	f.inventory.EXPECT().UpsertOffer(f.ctx, int64(10), int64(7), 120.0, 20.0, gomock.Any()).Return(boom)

	assert.ErrorIs(t, f.svc.Process(f.ctx, []int64{1, 2}), boom, "stops at the first failure; variant 11 and request 2 are not touched")
}

func TestStoreErrorsAreReturned(t *testing.T) {
	f := setup(t)
	boom := errors.New("db down")

	f.requests.EXPECT().Get(f.ctx, int64(1)).Return(models.SellerProductRequest{}, boom)
	assert.ErrorIs(t, f.svc.Process(f.ctx, []int64{1}), boom)

	f.requests.EXPECT().Get(f.ctx, int64(4)).Return(request(4, models.RequestArchived, models.StatusEnabled, 10), nil)
	f.requests.EXPECT().ApprovedVariants(f.ctx, int64(7), int64(3)).Return(nil, boom)
	assert.ErrorIs(t, f.svc.Process(f.ctx, []int64{4}), boom)
}

func TestMarkSyncedUsesTheUpdatedAtThatWasRead(t *testing.T) {
	f := setup(t)
	read := time.Date(2026, 11, 5, 12, 0, 0, 123456000, time.UTC)

	r := request(1, models.RequestApproved, models.StatusEnabled, 10)
	r.UpdatedAt = read

	f.requests.EXPECT().Get(f.ctx, int64(1)).Return(r, nil)
	f.inventory.EXPECT().UpsertOffer(f.ctx, int64(10), int64(7), 120.0, 20.0, gomock.Any()).Return(nil)
	f.requests.EXPECT().MarkSynced(f.ctx, int64(1), read).Return(nil) // not time.Now(): a change made meanwhile must stay unsynced

	assert.NoError(t, f.svc.Process(f.ctx, []int64{1}))
}

func TestAMarkSyncedFailureIsReturned(t *testing.T) {
	f := setup(t)
	boom := errors.New("db down")

	f.requests.EXPECT().Get(f.ctx, int64(1)).Return(request(1, models.RequestApproved, models.StatusEnabled, 10), nil)
	f.inventory.EXPECT().UpsertOffer(f.ctx, int64(10), int64(7), 120.0, 20.0, gomock.Any()).Return(nil)
	f.requests.EXPECT().MarkSynced(f.ctx, int64(1), gomock.Any()).Return(boom)

	assert.ErrorIs(t, f.svc.Process(f.ctx, []int64{1}), boom)
}

func TestReconcileAppliesEveryUnsyncedRequest(t *testing.T) {
	f := setup(t)

	f.requests.EXPECT().Unsynced(f.ctx, 20).Return([]int64{3, 5}, nil)
	f.requests.EXPECT().Get(f.ctx, int64(3)).Return(request(3, models.RequestApproved, models.StatusEnabled, 10), nil)
	f.inventory.EXPECT().UpsertOffer(f.ctx, int64(10), int64(7), 120.0, 20.0, gomock.Any()).Return(nil)
	f.requests.EXPECT().MarkSynced(f.ctx, int64(3), gomock.Any()).Return(nil)
	f.requests.EXPECT().Get(f.ctx, int64(5)).Return(request(5, models.RequestArchived, models.StatusEnabled, 11), nil)
	f.requests.EXPECT().ApprovedVariants(f.ctx, int64(7), int64(3)).Return([]int64{}, nil)
	f.inventory.EXPECT().Withdraw(f.ctx, int64(11), int64(7)).Return(nil)
	f.requests.EXPECT().MarkSynced(f.ctx, int64(5), gomock.Any()).Return(nil)

	applied, err := f.svc.Reconcile(f.ctx, 20)
	assert.NoError(t, err)
	assert.Equal(t, 2, applied)
}

func TestReconcileStopsAtTheFirstFailureAndReportsProgress(t *testing.T) {
	f := setup(t)
	down := errors.New("inventory-svc unreachable")

	f.requests.EXPECT().Unsynced(f.ctx, 20).Return([]int64{3, 5, 6}, nil)
	f.requests.EXPECT().Get(f.ctx, int64(3)).Return(request(3, models.RequestApproved, models.StatusEnabled, 10), nil)
	f.inventory.EXPECT().UpsertOffer(f.ctx, int64(10), int64(7), 120.0, 20.0, gomock.Any()).Return(nil)
	f.requests.EXPECT().MarkSynced(f.ctx, int64(3), gomock.Any()).Return(nil)
	f.requests.EXPECT().Get(f.ctx, int64(5)).Return(request(5, models.RequestApproved, models.StatusEnabled, 11), nil)
	f.inventory.EXPECT().UpsertOffer(f.ctx, int64(11), int64(7), 120.0, 20.0, gomock.Any()).Return(down)

	applied, err := f.svc.Reconcile(f.ctx, 20)
	assert.ErrorIs(t, err, down)
	assert.Equal(t, 1, applied, "request 3 was applied before inventory went away; 5 and 6 stay unsynced for the next pass")
}

func TestReconcileWithNothingToDoOrAStoreError(t *testing.T) {
	f := setup(t)

	f.requests.EXPECT().Unsynced(f.ctx, 20).Return([]int64{}, nil)

	applied, err := f.svc.Reconcile(f.ctx, 20)
	assert.NoError(t, err)
	assert.Zero(t, applied)

	boom := errors.New("db down")
	f.requests.EXPECT().Unsynced(f.ctx, 20).Return(nil, boom)

	_, err = f.svc.Reconcile(f.ctx, 20)
	assert.ErrorIs(t, err, boom)
}
