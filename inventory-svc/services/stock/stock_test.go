package stock

import (
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"

	"github.com/zopping/inventory-svc/common/apperr"
	"github.com/zopping/inventory-svc/filters"
	"github.com/zopping/inventory-svc/models"
	"github.com/zopping/inventory-svc/stores"
)

const (
	variant = int64(1)
	seller  = int64(7)
	house   = int64(99)
)

func invalid(params ...string) error { return gofrhttp.ErrorInvalidParam{Params: params} }

func ptr(f float64) *float64 { return &f }

func setup(t *testing.T) (*Service, *stores.MockStock, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	st := stores.NewMockStock(ctrl)

	return New(st, house), st, &gofr.Context{}
}

func TestPut_Validation(t *testing.T) {
	svc, _, ctx := setup(t)
	neg, zero := int64(-1), int64(0)

	tests := []struct {
		name string
		req  models.StockRequest
		want string
	}{
		{"negative quantity", models.StockRequest{Quantity: -1}, "quantity"},
		{"negative threshold", models.StockRequest{LowStockThreshold: &neg}, "lowStockThreshold"},
		{"negative unit", models.StockRequest{Unit: -2}, "unit"},
		{"bad user", models.StockRequest{UserID: &zero}, "userId"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.req
			_, err := svc.Put(ctx, variant, seller, &req)
			assert.Equal(t, invalid(tc.want), err)
		})
	}
}

func TestPut_DefaultsUnitAndKeysBySeller(t *testing.T) {
	svc, st, ctx := setup(t)

	st.EXPECT().Put(ctx, variant, seller, gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, _, _ int64, r *models.StockRequest) (models.Stock, error) {
			assert.Equal(t, 1, r.Unit, "unit defaults to 1")

			return models.Stock{VariantID: variant, SellerID: seller}, nil
		})

	got, err := svc.Put(ctx, variant, seller, &models.StockRequest{Quantity: 5})
	assert.NoError(t, err)
	assert.Equal(t, seller, got.SellerID)
}

func TestPut_CannotCreateAnOfferOrChangeItsStatus(t *testing.T) {
	svc, st, ctx := setup(t)

	st.EXPECT().Put(ctx, variant, int64(8), gomock.Any()).Return(models.Stock{}, apperr.ErrNotFound)

	_, err := svc.Put(ctx, variant, 8, &models.StockRequest{Quantity: 5})
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "1"}, err,
		"a seller with no approved offer cannot stock the variant: only the approval flow creates offers")

	// the request has no status field at all, so a body naming one cannot reach the store
	_, hasStatus := reflect.TypeOf(models.StockRequest{}).FieldByName("Status")
	assert.False(t, hasStatus, "status belongs to the catalogue's approval and visibility flow")
}

func TestAdjust_Rules(t *testing.T) {
	svc, st, ctx := setup(t)
	zero := int64(0)

	_, err := svc.Adjust(ctx, variant, seller, &models.AdjustRequest{Delta: 0})
	assert.Equal(t, invalid("delta"), err)

	_, err = svc.Adjust(ctx, variant, seller, &models.AdjustRequest{Delta: 1, ReferenceID: "GRN-1"})
	assert.Equal(t, invalid("referenceType", "referenceId"), err, "an id without a type")

	_, err = svc.Adjust(ctx, variant, seller, &models.AdjustRequest{Delta: 1, Reason: "STOLEN"})
	assert.Equal(t, invalid("reason"), err, "only the known movement reasons are accepted")

	_, err = svc.Adjust(ctx, variant, seller, &models.AdjustRequest{Delta: 1, UserID: &zero})
	assert.Equal(t, invalid("userId"), err)

	st.EXPECT().Adjust(ctx, variant, seller, gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, _, _ int64, r *models.AdjustRequest) (models.Stock, error) {
			assert.Equal(t, models.ReasonDamaged, r.Reason, "reason is case-insensitive")

			return models.Stock{}, nil
		})

	_, err = svc.Adjust(ctx, variant, seller, &models.AdjustRequest{Delta: -1, Reason: " damaged "})
	assert.NoError(t, err)

	st.EXPECT().Adjust(ctx, variant, seller, gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, _, _ int64, r *models.AdjustRequest) (models.Stock, error) {
			assert.Equal(t, models.ReasonOther, r.Reason, "reason defaults to OTHER")

			return models.Stock{}, nil
		})

	_, err = svc.Adjust(ctx, variant, seller, &models.AdjustRequest{Delta: 1})
	assert.NoError(t, err)
}

func TestAdjust_InsufficientStockIsA409NamingTheSeller(t *testing.T) {
	svc, st, ctx := setup(t)
	req := &models.AdjustRequest{Delta: -5}

	st.EXPECT().Adjust(ctx, variant, seller, req).Return(models.Stock{},
		&apperr.InsufficientStock{VariantID: variant, SellerID: seller, Requested: 5, Available: 2})

	_, err := svc.Adjust(ctx, variant, seller, req)
	assert.EqualError(t, err, "insufficient stock for variant 1 from seller 7: requested 5, available 2")

	var coded interface{ StatusCode() int }

	assert.ErrorAs(t, err, &coded)
	assert.Equal(t, 409, coded.StatusCode())

	st.EXPECT().Adjust(ctx, variant, int64(8), req).Return(models.Stock{}, apperr.ErrNotFound)

	_, err = svc.Adjust(ctx, variant, 8, req)
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "1"}, err, "that seller has no row for the variant")
}

func TestSetPrice_Validation(t *testing.T) {
	svc, _, ctx := setup(t)
	zero := int64(0)

	tests := []struct {
		name string
		req  models.PriceRequest
		want string
	}{
		{"negative mrp", models.PriceRequest{MRP: ptr(-1)}, "mrp"},
		{"negative discount", models.PriceRequest{MRP: ptr(100), Discount: -1}, "discount"},
		{"discount without an mrp", models.PriceRequest{Discount: 5}, "mrp"},
		{"discount above mrp", models.PriceRequest{MRP: ptr(100), Discount: 100.01}, "discount"},
		{"negative cost price", models.PriceRequest{MRP: ptr(100), CostPrice: ptr(-0.5)}, "costPrice"},
		{"bad user", models.PriceRequest{MRP: ptr(100), UserID: &zero}, "userId"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.req
			_, err := svc.SetPrice(ctx, variant, seller, &req)
			assert.Equal(t, invalid(tc.want), err)
		})
	}
}

func TestSetPrice_AcceptsEdgeValuesTrimsAndMapsErrors(t *testing.T) {
	svc, st, ctx := setup(t)

	st.EXPECT().SetPrice(ctx, variant, seller, gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, _, _ int64, r *models.PriceRequest) (models.Stock, error) {
			assert.Equal(t, "GST 5%", r.Tax)
			assert.Equal(t, "0902", r.HSNCode)

			return models.Stock{}, nil
		})

	_, err := svc.SetPrice(ctx, variant, seller, &models.PriceRequest{MRP: ptr(50), Discount: 50, Tax: " GST 5% ", HSNCode: " 0902 "})
	assert.NoError(t, err, "a discount equal to the mrp is allowed")

	st.EXPECT().SetPrice(ctx, variant, seller, gomock.Any()).Return(models.Stock{}, apperr.ErrNotFound)

	_, err = svc.SetPrice(ctx, variant, seller, &models.PriceRequest{MRP: ptr(10)})
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "1"}, err, "pricing an offer that has no stock row")

	st.EXPECT().SetPrice(ctx, variant, seller, gomock.Any()).Return(models.Stock{}, &apperr.InvalidRef{Constraint: "stock_discount_within_mrp"})

	_, err = svc.SetPrice(ctx, variant, seller, &models.PriceRequest{MRP: ptr(10)})
	assert.Equal(t, invalid("discount"), err, "the database constraint is the backstop")
}

func TestUpsertOffer(t *testing.T) {
	svc, st, ctx := setup(t)
	zero := int64(0)

	for _, tc := range []struct {
		req  models.OfferRequest
		want string
	}{
		{models.OfferRequest{MRP: -1}, "mrp"},
		{models.OfferRequest{MRP: 100, Discount: -1}, "discount"},
		{models.OfferRequest{MRP: 100, Discount: 101}, "discount"},
		{models.OfferRequest{MRP: 100, UserID: &zero}, "userId"},
	} {
		req := tc.req
		_, err := svc.UpsertOffer(ctx, variant, seller, &req)
		assert.Equal(t, invalid(tc.want), err)
	}

	req := &models.OfferRequest{MRP: 100, Discount: 10}
	st.EXPECT().UpsertOffer(ctx, variant, seller, req).Return(models.Stock{VariantID: variant, SellerID: seller}, nil)

	got, err := svc.UpsertOffer(ctx, variant, seller, req)
	assert.NoError(t, err)
	assert.Equal(t, seller, got.SellerID)
}

func TestWithdrawGetListMovementsPriceLog(t *testing.T) {
	svc, st, ctx := setup(t)

	st.EXPECT().Withdraw(ctx, variant, seller).Return(nil)
	assert.NoError(t, svc.Withdraw(ctx, variant, seller))

	st.EXPECT().Withdraw(ctx, variant, int64(8)).Return(apperr.ErrNotFound)
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "1"}, svc.Withdraw(ctx, variant, 8))

	st.EXPECT().Get(ctx, variant, seller).Return(models.Stock{VariantID: variant, SellerID: seller}, nil)

	got, err := svc.Get(ctx, variant, seller)
	assert.NoError(t, err)
	assert.Equal(t, seller, got.SellerID)

	_, err = svc.List(ctx, &filters.StockFilter{Status: "active"})
	assert.Equal(t, invalid("status"), err)

	f := &filters.StockFilter{VariantID: 1, Limit: 20}
	st.EXPECT().List(ctx, f).Return([]models.Stock{{VariantID: 1}}, 1, nil)

	list, err := svc.List(ctx, f)
	assert.NoError(t, err)
	assert.Equal(t, 1, list.Count)

	m := &filters.MovementFilter{VariantID: 1, SellerID: 7, Limit: 10}
	st.EXPECT().Movements(ctx, m).Return([]models.Movement{{ID: 1}}, 1, nil)

	moves, err := svc.Movements(ctx, m)
	assert.NoError(t, err)
	assert.Equal(t, 1, moves.Count)

	p := &filters.PriceLogFilter{VariantID: 1, SellerID: 7, Limit: 20}
	st.EXPECT().PriceLog(ctx, p).Return([]models.PriceChange{{ID: 1}}, 1, nil)

	changes, err := svc.PriceLog(ctx, p)
	assert.NoError(t, err)
	assert.Equal(t, 1, changes.Count)
}

func TestValidReasonAndStatus(t *testing.T) {
	for _, r := range []string{"RECEIVED", "RETURNED_TO_SELLER", "EXPIRED", "DAMAGED", "RETURNED", "SOLD", "RECOUNTED", "OTHER"} {
		assert.True(t, models.ValidReason(r), r)
	}

	assert.False(t, models.ValidReason("RECEIVED_FROM_DC"), "no distribution-centre reasons in a marketplace")
	assert.True(t, models.ValidStatus("HIDDEN"))
	assert.False(t, models.ValidStatus("hidden"))
}

func offer(sellerID int64, price float64, available int64) models.Offer {
	p := price

	return models.Offer{SellerID: sellerID, Available: available, InStock: available > 0, Unit: 1, MRP: &p, SellingPrice: &p}
}

func TestAvailability_RejectsAnEmptyRequest(t *testing.T) {
	svc, _, ctx := setup(t)

	_, err := svc.Availability(ctx, nil)
	assert.Equal(t, invalid("variantIds"), err)
}

func TestAvailability_RanksOffersAndPicksTheBest(t *testing.T) {
	unpriced := models.Offer{SellerID: 4, Available: 50, InStock: true}
	unlimited := offer(6, 100, 0)
	unlimited.UnlimitedStock, unlimited.InStock = true, true

	tests := []struct {
		name       string
		offers     []models.Offer
		wantOrder  []int64
		wantWinner int64 // 0 = none
	}{
		{"cheapest in-stock wins", []models.Offer{offer(1, 120, 5), offer(2, 100, 5), offer(3, 110, 5)}, []int64{2, 3, 1}, 2},
		{"an out-of-stock cheaper offer is listed but cannot win", []models.Offer{offer(1, 100, 0), offer(2, 120, 4)}, []int64{1, 2}, 2},
		{"nobody has stock", []models.Offer{offer(1, 100, 0), offer(2, 120, 0)}, []int64{1, 2}, 0},
		{"an offer with no price sorts last and never wins", []models.Offer{unpriced, offer(2, 500, 1)}, []int64{2, 4}, 2},
		{"only unpriced offers", []models.Offer{unpriced}, []int64{4}, 0},
		{"the house seller wins a price tie", []models.Offer{offer(1, 100, 99), offer(house, 100, 1)}, []int64{house, 1}, house},
		{"on a tie without the house, more stock wins", []models.Offer{offer(1, 100, 3), offer(2, 100, 9)}, []int64{2, 1}, 2},
		{"unlimited stock beats finite stock at the same price", []models.Offer{offer(1, 100, 1000), unlimited}, []int64{6, 1}, 6},
		{"a full tie goes to the lower seller id", []models.Offer{offer(5, 100, 4), offer(3, 100, 4)}, []int64{3, 5}, 3},
		{"no offers", []models.Offer{}, []int64{}, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, st, ctx := setup(t)

			st.EXPECT().Offers(ctx, []int64{1}).Return([]models.VariantAvailability{{VariantID: 1, Offers: tc.offers}}, nil)

			got, err := svc.Availability(ctx, []int64{1})
			assert.NoError(t, err)
			assert.Len(t, got, 1)

			order := []int64{}
			for _, o := range got[0].Offers {
				order = append(order, o.SellerID)
			}

			assert.Equal(t, tc.wantOrder, order)

			if tc.wantWinner == 0 {
				assert.Nil(t, got[0].BestOffer)

				return
			}

			assert.NotNil(t, got[0].BestOffer)
			assert.Equal(t, tc.wantWinner, got[0].BestOffer.SellerID)
		})
	}
}

func TestAvailability_NoHouseSellerConfigured(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	st := stores.NewMockStock(ctrl)
	svc := New(st, 0)
	ctx := &gofr.Context{}

	st.EXPECT().Offers(ctx, []int64{1}).Return([]models.VariantAvailability{{VariantID: 1, Offers: []models.Offer{
		offer(2, 100, 5), offer(1, 100, 5),
	}}}, nil)

	got, err := svc.Availability(ctx, []int64{1})
	assert.NoError(t, err)
	assert.Equal(t, int64(1), got[0].BestOffer.SellerID, "with no house seller a tie falls through to the seller id, never to seller 0")
}
