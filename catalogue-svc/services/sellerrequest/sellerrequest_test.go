package sellerrequest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"
	"gofr.dev/pkg/gofr/logging"

	"github.com/zopping/catalogue-svc/common/apperr"
	"github.com/zopping/catalogue-svc/common/caller"
	"github.com/zopping/catalogue-svc/common/events"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/stores"
)

const topic = "seller-product-change"

type fixture struct {
	svc         *Service
	requests    *stores.MockSellerRequest
	permissible *stores.MockPermissibleSeller
	sellers     *stores.MockSeller
	products    *stores.MockProduct
	variants    *stores.MockVariant
	publisher   *events.MockPublisher
	ctx         *gofr.Context
}

func setup(t *testing.T) fixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	f := fixture{
		requests: stores.NewMockSellerRequest(ctrl), permissible: stores.NewMockPermissibleSeller(ctrl),
		sellers: stores.NewMockSeller(ctrl), products: stores.NewMockProduct(ctrl), variants: stores.NewMockVariant(ctrl),
		publisher: events.NewMockPublisher(ctrl),
		ctx:       &gofr.Context{ContextLogger: logging.ContextLoggerFor(context.Background(), logging.NewMockLogger(logging.DEBUG))},
	}
	f.svc = New(f.requests, f.permissible, f.sellers, f.products, f.variants, f.publisher, topic)

	return f
}

func invalid(param string) error { return gofrhttp.ErrorInvalidParam{Params: []string{param}} }

func items(vs ...int64) []models.SellerProductItem {
	out := make([]models.SellerProductItem, len(vs))
	for i, v := range vs {
		out[i] = models.SellerProductItem{VariantID: v, MRP: 100, Discount: 10}
	}

	return out
}

// happy primes the stores so that seller 7 may file a request for product 3, whose live variants are 10 and 11.
func (f fixture) happy() {
	f.sellers.EXPECT().Get(f.ctx, int64(7)).Return(models.Seller{ID: 7, Status: models.SellerStatusActive}, nil)
	f.products.EXPECT().Get(f.ctx, int64(3)).Return(models.Product{ID: 3}, nil)
	f.permissible.EXPECT().Allowed(f.ctx, int64(7), int64(3)).Return(true, nil)
	f.variants.EXPECT().ListByProductIDs(f.ctx, []int64{3}).Return([]models.Variant{{ID: 10}, {ID: 11}}, nil)
}

func TestCreate_Basics(t *testing.T) {
	f := setup(t)

	_, err := f.svc.Create(f.ctx, &models.SellerProductRequestInput{ProductID: 3, Items: items(10)})
	assert.Equal(t, invalid("sellerId"), err)

	_, err = f.svc.Create(f.ctx, &models.SellerProductRequestInput{SellerID: 7, Items: items(10)})
	assert.Equal(t, invalid("productId"), err)

	_, err = f.svc.Create(f.ctx, &models.SellerProductRequestInput{SellerID: 7, ProductID: 3, Status: "APPROVED", Items: items(10)})
	assert.Equal(t, invalid("status"), err, "a seller cannot file a request that is already approved")
}

func TestCreate_SellerAndProductRules(t *testing.T) {
	f := setup(t)
	in := func() *models.SellerProductRequestInput {
		return &models.SellerProductRequestInput{SellerID: 7, ProductID: 3, Items: items(10)}
	}

	var forbidden *apperr.Forbidden

	f.sellers.EXPECT().Get(f.ctx, int64(7)).Return(models.Seller{}, apperr.ErrNotFound)

	_, err := f.svc.Create(f.ctx, in())
	assert.Equal(t, invalid("sellerId"), err, "unknown seller")

	f.sellers.EXPECT().Get(f.ctx, int64(7)).Return(models.Seller{ID: 7, Status: models.SellerStatusSuspended}, nil)

	_, err = f.svc.Create(f.ctx, in())
	assert.ErrorAs(t, err, &forbidden, "a suspended seller")

	f.sellers.EXPECT().Get(f.ctx, int64(7)).Return(models.Seller{ID: 7, Status: models.SellerStatusActive}, nil)
	f.products.EXPECT().Get(f.ctx, int64(3)).Return(models.Product{}, apperr.ErrNotFound)

	_, err = f.svc.Create(f.ctx, in())
	assert.Equal(t, invalid("productId"), err, "unknown product")

	f.sellers.EXPECT().Get(f.ctx, int64(7)).Return(models.Seller{ID: 7, Status: models.SellerStatusActive}, nil)
	f.products.EXPECT().Get(f.ctx, int64(3)).Return(models.Product{ID: 3}, nil)
	f.permissible.EXPECT().Allowed(f.ctx, int64(7), int64(3)).Return(false, nil)

	_, err = f.svc.Create(f.ctx, in())
	assert.ErrorAs(t, err, &forbidden, "a seller who is not on the product's whitelist")
	assert.Equal(t, "the seller is not permitted to sell this product", forbidden.Reason)
}

func TestCreate_ItemRules(t *testing.T) {
	bad := map[string][]models.SellerProductItem{
		"no items":                     nil,
		"a variant of another product": items(99),
		"the same variant twice":       items(10, 10),
		"negative price":               {{VariantID: 10, MRP: -1}},
		"negative discount":            {{VariantID: 10, MRP: 100, Discount: -1}},
		"discount above the price":     {{VariantID: 10, MRP: 100, Discount: 100.5}},
	}

	for name, its := range bad {
		t.Run(name, func(t *testing.T) {
			f := setup(t)

			if len(its) > 0 {
				f.happy()
			} else {
				f.sellers.EXPECT().Get(f.ctx, int64(7)).Return(models.Seller{ID: 7, Status: models.SellerStatusActive}, nil)
				f.products.EXPECT().Get(f.ctx, int64(3)).Return(models.Product{ID: 3}, nil)
				f.permissible.EXPECT().Allowed(f.ctx, int64(7), int64(3)).Return(true, nil)
			}

			_, err := f.svc.Create(f.ctx, &models.SellerProductRequestInput{SellerID: 7, ProductID: 3, Items: its})
			assert.Equal(t, invalid("items"), err)
		})
	}
}

func TestCreate_SuccessDefaultsToPending(t *testing.T) {
	f := setup(t)
	f.happy()

	f.requests.EXPECT().Create(f.ctx, gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, in *models.SellerProductRequestInput) (models.SellerProductRequest, error) {
			assert.Equal(t, models.RequestPending, in.Status)

			return models.SellerProductRequest{ID: 1, Status: in.Status}, nil
		})

	got, err := f.svc.Create(f.ctx, &models.SellerProductRequestInput{SellerID: 7, ProductID: 3, Items: items(10, 11)})
	assert.NoError(t, err)
	assert.Equal(t, int64(1), got.ID)
}

func TestUpdate(t *testing.T) {
	f := setup(t)
	current := models.SellerProductRequest{ID: 5, SellerID: 7, ProductID: 3, Status: models.RequestRejected}

	f.requests.EXPECT().Get(f.ctx, int64(404)).Return(models.SellerProductRequest{}, apperr.ErrNotFound)

	_, err := f.svc.Update(f.ctx, 404, &models.SellerProductRequestInput{Items: items(10)})
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "404"}, err)

	var forbidden *apperr.Forbidden

	f.requests.EXPECT().Get(f.ctx, int64(5)).Return(current, nil)
	f.sellers.EXPECT().Get(f.ctx, int64(7)).Return(models.Seller{ID: 7, Status: models.SellerStatusActive}, nil)
	f.permissible.EXPECT().Allowed(f.ctx, int64(7), int64(3)).Return(false, nil)

	_, err = f.svc.Update(f.ctx, 5, &models.SellerProductRequestInput{Items: items(10)})
	assert.ErrorAs(t, err, &forbidden, "a seller removed from the whitelist cannot resubmit")

	f.requests.EXPECT().Get(f.ctx, int64(5)).Return(current, nil)
	f.sellers.EXPECT().Get(f.ctx, int64(7)).Return(models.Seller{ID: 7, Status: models.SellerStatusActive}, nil)
	f.permissible.EXPECT().Allowed(f.ctx, int64(7), int64(3)).Return(true, nil)
	f.variants.EXPECT().ListByProductIDs(f.ctx, []int64{3}).Return([]models.Variant{{ID: 10}}, nil)
	f.requests.EXPECT().Update(f.ctx, int64(5), gomock.Any()).Return(models.SellerProductRequest{ID: 5, Status: models.RequestPending}, nil)

	got, err := f.svc.Update(f.ctx, 5, &models.SellerProductRequestInput{SellerID: 999, ProductID: 999, Items: items(10)})
	assert.NoError(t, err)
	assert.Equal(t, int64(7), got.SellerID, "the seller and product come from the stored request, never from the body")
	assert.Equal(t, int64(3), got.ProductID)

	f.requests.EXPECT().Get(f.ctx, int64(5)).Return(current, nil)
	f.sellers.EXPECT().Get(f.ctx, int64(7)).Return(models.Seller{ID: 7, Status: models.SellerStatusActive}, nil)
	f.permissible.EXPECT().Allowed(f.ctx, int64(7), int64(3)).Return(true, nil)
	f.variants.EXPECT().ListByProductIDs(f.ctx, []int64{3}).Return([]models.Variant{{ID: 10}}, nil)
	f.requests.EXPECT().Update(f.ctx, int64(5), gomock.Any()).Return(models.SellerProductRequest{},
		&apperr.StateConflict{Reason: "a approved request cannot be edited"})

	_, err = f.svc.Update(f.ctx, 5, &models.SellerProductRequestInput{Items: items(10)})
	assert.EqualError(t, err, "a approved request cannot be edited", "an approved request is read-only")
}

func TestApproval_Rejection(t *testing.T) {
	f := setup(t)

	_, err := f.svc.Approval(f.ctx, 5, &models.ApprovalRequest{Status: "MAYBE"})
	assert.Equal(t, invalid("status"), err)

	_, err = f.svc.Approval(f.ctx, 5, &models.ApprovalRequest{Status: "rejected", RejectionReason: "  "})
	assert.Equal(t, invalid("rejectionReason"), err, "a rejection needs a reason")

	f.requests.EXPECT().Reject(f.ctx, int64(5), "wrong images").Return(models.SellerProductRequest{ID: 5, Status: models.RequestRejected}, nil)

	got, err := f.svc.Approval(f.ctx, 5, &models.ApprovalRequest{Status: " rejected ", RejectionReason: " wrong images "})
	assert.NoError(t, err)
	assert.Equal(t, models.RequestRejected, got.Status)
}

func TestApproval_ApprovePublishesEveryChangedRequest(t *testing.T) {
	f := setup(t)
	req := models.SellerProductRequest{ID: 9, SellerID: 7, ProductID: 3, Status: models.RequestPending}

	f.requests.EXPECT().Get(f.ctx, int64(9)).Return(req, nil).Times(2)
	f.sellers.EXPECT().Get(f.ctx, int64(7)).Return(models.Seller{ID: 7, Status: models.SellerStatusActive}, nil)
	f.requests.EXPECT().Approve(f.ctx, int64(9)).Return([]int64{4, 9}, nil)
	f.publisher.EXPECT().Publish(f.ctx, topic, models.SellerProductChangeEvent{SellerProductRequestIDs: []int64{4, 9}}).Return(nil)

	_, err := f.svc.Approval(f.ctx, 9, &models.ApprovalRequest{Status: "APPROVED"})
	assert.NoError(t, err, "the archived request (4) and the new one (9) go in one event")
}

func TestApproval_Failures(t *testing.T) {
	f := setup(t)
	req := models.SellerProductRequest{ID: 9, SellerID: 7, ProductID: 3}

	f.requests.EXPECT().Get(f.ctx, int64(404)).Return(models.SellerProductRequest{}, apperr.ErrNotFound)

	_, err := f.svc.Approval(f.ctx, 404, &models.ApprovalRequest{Status: "APPROVED"})
	assert.Equal(t, gofrhttp.ErrorEntityNotFound{Name: "id", Value: "404"}, err)

	var forbidden *apperr.Forbidden

	f.requests.EXPECT().Get(f.ctx, int64(9)).Return(req, nil)
	f.sellers.EXPECT().Get(f.ctx, int64(7)).Return(models.Seller{ID: 7, Status: models.SellerStatusSuspended}, nil)

	_, err = f.svc.Approval(f.ctx, 9, &models.ApprovalRequest{Status: "APPROVED"})
	assert.ErrorAs(t, err, &forbidden, "a suspended seller's request cannot go live")

	f.requests.EXPECT().Get(f.ctx, int64(9)).Return(req, nil)
	f.sellers.EXPECT().Get(f.ctx, int64(7)).Return(models.Seller{ID: 7, Status: models.SellerStatusActive}, nil)
	f.requests.EXPECT().Approve(f.ctx, int64(9)).Return(nil, &apperr.StateConflict{Reason: "a draft request cannot be approved"})

	_, err = f.svc.Approval(f.ctx, 9, &models.ApprovalRequest{Status: "APPROVED"})
	assert.EqualError(t, err, "a draft request cannot be approved")

	f.requests.EXPECT().Get(f.ctx, int64(9)).Return(req, nil)
	f.sellers.EXPECT().Get(f.ctx, int64(7)).Return(models.Seller{ID: 7, Status: models.SellerStatusActive}, nil)
	f.requests.EXPECT().Approve(f.ctx, int64(9)).Return([]int64{9}, nil)
	f.publisher.EXPECT().Publish(f.ctx, topic, gomock.Any()).Return(events.ErrNotConfigured)

	_, err = f.svc.Approval(f.ctx, 9, &models.ApprovalRequest{Status: "APPROVED"})
	assert.ErrorIs(t, err, ErrEventNotPublished, "approved and saved, but the caller is told the event was not sent so they repeat it")
}

func TestVisibility(t *testing.T) {
	f := setup(t)

	_, err := f.svc.Visibility(f.ctx, 9, &models.VisibilityRequest{ProductStatus: "GONE"})
	assert.Equal(t, invalid("productStatus"), err)

	f.requests.EXPECT().Get(f.ctx, int64(9)).Return(models.SellerProductRequest{ID: 9}, nil).Times(2)
	f.requests.EXPECT().SetVisibility(f.ctx, int64(9), "HIDDEN").Return(models.SellerProductRequest{ID: 9, ProductStatus: "HIDDEN"}, nil)
	f.publisher.EXPECT().Publish(f.ctx, topic, models.SellerProductChangeEvent{SellerProductRequestIDs: []int64{9}}).Return(nil)

	got, err := f.svc.Visibility(f.ctx, 9, &models.VisibilityRequest{ProductStatus: "hidden"})
	assert.NoError(t, err)
	assert.Equal(t, "HIDDEN", got.ProductStatus)

	f.requests.EXPECT().SetVisibility(f.ctx, int64(9), "ENABLED").Return(models.SellerProductRequest{},
		&apperr.StateConflict{Reason: "a pending request cannot be shown or hidden"})

	_, err = f.svc.Visibility(f.ctx, 9, &models.VisibilityRequest{ProductStatus: "ENABLED"})
	assert.EqualError(t, err, "a pending request cannot be shown or hidden")
}

func TestListGetDelete(t *testing.T) {
	f := setup(t)

	_, err := f.svc.List(f.ctx, &filters.SellerProductRequestFilter{Status: "MAYBE"})
	assert.Equal(t, invalid("status"), err)

	flt := &filters.SellerProductRequestFilter{Status: "ARCHIVED", Limit: 20}
	f.requests.EXPECT().List(f.ctx, flt).Return([]models.SellerProductRequest{{ID: 1}}, 1, nil)

	got, err := f.svc.List(f.ctx, flt)
	assert.NoError(t, err)
	assert.Equal(t, 1, got.Count)

	f.requests.EXPECT().Get(f.ctx, int64(1)).Return(models.SellerProductRequest{ID: 1}, nil).Times(2) // Get, and Delete's ownership check

	r, err := f.svc.Get(f.ctx, 1)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), r.ID)

	f.requests.EXPECT().Delete(f.ctx, int64(1)).Return(&apperr.StateConflict{Reason: "a approved request cannot be deleted"})
	assert.EqualError(t, f.svc.Delete(f.ctx, 1), "a approved request cannot be deleted")
}

// as returns a context for a call the gateway forwarded with the given identity headers.
func as(headers map[string]string) *gofr.Context {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}

	var ctx *gofr.Context

	caller.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		ctx = &gofr.Context{
			Request:       gofrhttp.NewRequest(req),
			ContextLogger: logging.ContextLoggerFor(context.Background(), logging.NewMockLogger(logging.DEBUG)),
		}
	})).ServeHTTP(httptest.NewRecorder(), r)

	return ctx
}

func seller7() *gofr.Context { return as(map[string]string{"X-Seller-Id": "7"}) }

func TestASellerCanOnlyFileForThemselves(t *testing.T) {
	f := setup(t)

	var forbidden *apperr.Forbidden

	_, err := f.svc.Create(seller7(), &models.SellerProductRequestInput{SellerID: 8, ProductID: 3, Items: items(10)})
	assert.ErrorAs(t, err, &forbidden)
	assert.Equal(t, "you can only act for your own seller", forbidden.Reason)

	ctx := seller7()
	f.sellers.EXPECT().Get(ctx, int64(7)).Return(models.Seller{ID: 7, Status: models.SellerStatusActive}, nil)
	f.products.EXPECT().Get(ctx, int64(3)).Return(models.Product{ID: 3}, nil)
	f.permissible.EXPECT().Allowed(ctx, int64(7), int64(3)).Return(true, nil)
	f.variants.EXPECT().ListByProductIDs(ctx, []int64{3}).Return([]models.Variant{{ID: 10}}, nil)
	f.requests.EXPECT().Create(ctx, gomock.Any()).Return(models.SellerProductRequest{ID: 1, SellerID: 7}, nil)

	_, err = f.svc.Create(ctx, &models.SellerProductRequestInput{SellerID: 7, ProductID: 3, Items: items(10)})
	assert.NoError(t, err, "their own seller is fine")
}

func TestASellerCannotSeeEditOrDeleteAnotherSellersRequest(t *testing.T) {
	f := setup(t)
	theirs := models.SellerProductRequest{ID: 5, SellerID: 8, ProductID: 3, Status: models.RequestDraft}
	notFound := gofrhttp.ErrorEntityNotFound{Name: "id", Value: "5"}
	ctx := seller7()

	f.requests.EXPECT().Get(ctx, int64(5)).Return(theirs, nil).Times(4)

	_, err := f.svc.Get(ctx, 5)
	assert.Equal(t, notFound, err, "reported as not found, so ids cannot be probed")

	_, err = f.svc.Update(ctx, 5, &models.SellerProductRequestInput{Items: items(10)})
	assert.Equal(t, notFound, err)

	assert.Equal(t, notFound, f.svc.Delete(ctx, 5))

	_, err = f.svc.Visibility(ctx, 5, &models.VisibilityRequest{ProductStatus: "HIDDEN"})
	assert.Equal(t, notFound, err)
	// no Update, Delete or SetVisibility expectation exists: the strict mock fails the test if the store were touched
}

func TestASellerSeesAndManagesTheirOwnRequest(t *testing.T) {
	f := setup(t)
	mine := models.SellerProductRequest{ID: 5, SellerID: 7, ProductID: 3, Status: models.RequestApproved}
	ctx := seller7()

	f.requests.EXPECT().Get(ctx, int64(5)).Return(mine, nil).Times(3)

	got, err := f.svc.Get(ctx, 5)
	assert.NoError(t, err)
	assert.Equal(t, int64(5), got.ID)

	f.requests.EXPECT().SetVisibility(ctx, int64(5), "HIDDEN").Return(mine, nil)
	f.publisher.EXPECT().Publish(ctx, topic, gomock.Any()).Return(nil)

	_, err = f.svc.Visibility(ctx, 5, &models.VisibilityRequest{ProductStatus: "HIDDEN"})
	assert.NoError(t, err, "a seller may hide their own live offer")

	f.requests.EXPECT().Delete(ctx, int64(5)).Return(&apperr.StateConflict{Reason: "a approved request cannot be deleted"})
	assert.EqualError(t, f.svc.Delete(ctx, 5), "a approved request cannot be deleted")
}

func TestASellersListIsAlwaysLimitedToTheirOwn(t *testing.T) {
	f := setup(t)
	ctx := seller7()

	f.requests.EXPECT().List(ctx, &filters.SellerProductRequestFilter{SellerID: 7, Limit: 20}).Return([]models.SellerProductRequest{}, 0, nil)

	_, err := f.svc.List(ctx, &filters.SellerProductRequestFilter{SellerID: 8, Limit: 20})
	assert.NoError(t, err, "asking for seller 8 still lists seller 7's")
}

func TestOnlyAnAdministratorCanApprove(t *testing.T) {
	f := setup(t)

	var forbidden *apperr.Forbidden

	_, err := f.svc.Approval(seller7(), 5, &models.ApprovalRequest{Status: "APPROVED"})
	assert.ErrorAs(t, err, &forbidden, "a seller must not approve their own request")

	_, err = f.svc.Approval(seller7(), 5, &models.ApprovalRequest{Status: "REJECTED", RejectionReason: "x"})
	assert.ErrorAs(t, err, &forbidden)
}

func TestAdminsAndInternalCallersAreNotRestricted(t *testing.T) {
	f := setup(t)
	theirs := models.SellerProductRequest{ID: 5, SellerID: 8}

	for name, ctx := range map[string]*gofr.Context{
		"admin":    as(map[string]string{"X-Seller-Scope": "all", "X-Seller-Id": "7"}),
		"internal": as(nil),
	} {
		f.requests.EXPECT().Get(ctx, int64(5)).Return(theirs, nil)

		got, err := f.svc.Get(ctx, 5)
		assert.NoError(t, err, name)
		assert.Equal(t, int64(8), got.SellerID, name)
	}

	f.requests.EXPECT().Reject(gomock.Any(), int64(5), "x").Return(models.SellerProductRequest{ID: 5}, nil)

	_, err := f.svc.Approval(as(map[string]string{"X-Seller-Scope": "all"}), 5, &models.ApprovalRequest{Status: "REJECTED", RejectionReason: "x"})
	assert.NoError(t, err)
}
