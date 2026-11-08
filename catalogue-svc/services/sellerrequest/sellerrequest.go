// Package sellerrequest holds the seller product request workflow: a permitted seller asks to sell a shared product at
// their own price, an admin approves or rejects, and approval publishes the event that makes the offer live.
package sellerrequest

import (
	"errors"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/common/apperr"
	"github.com/zopping/catalogue-svc/common/caller"
	"github.com/zopping/catalogue-svc/common/events"
	"github.com/zopping/catalogue-svc/common/httpx"
	"github.com/zopping/catalogue-svc/filters"
	"github.com/zopping/catalogue-svc/models"
	"github.com/zopping/catalogue-svc/services"
	"github.com/zopping/catalogue-svc/stores"
)

const maxItems = 100

// ErrEventNotPublished is returned when a request was approved (or its visibility changed) but the event could not be
// sent. The change is saved; repeating the call re-sends the event.
var ErrEventNotPublished = errors.New(
	"saved, but the change event could not be published: it will be applied by the next reconcile, or repeat the call to re-send it")

var _ services.SellerRequest = (*Service)(nil)

// Service implements services.SellerRequest.
type Service struct {
	requests    stores.SellerRequest
	permissible stores.PermissibleSeller
	sellers     stores.Seller
	products    stores.Product
	variants    stores.Variant
	publisher   events.Publisher
	topic       string
}

// New returns a seller request service that publishes change events to topic.
func New(requests stores.SellerRequest, permissible stores.PermissibleSeller, sellers stores.Seller, products stores.Product,
	variants stores.Variant, publisher events.Publisher, topic string) *Service {
	return &Service{
		requests: requests, permissible: permissible, sellers: sellers, products: products, variants: variants,
		publisher: publisher, topic: topic,
	}
}

// status returns the status a seller may submit: DRAFT or PENDING, defaulting to PENDING.
func status(in *models.SellerProductRequestInput) error {
	if in.Status == "" {
		in.Status = models.RequestPending
	}

	if in.Status != models.RequestDraft && in.Status != models.RequestPending {
		return httpx.InvalidParam("status")
	}

	return nil
}

// activeSeller returns 400 for an unknown seller and 403 for a suspended one.
func (s *Service) activeSeller(ctx *gofr.Context, sellerID int64) error {
	seller, err := s.sellers.Get(ctx, sellerID)
	if errors.Is(err, apperr.ErrNotFound) || errors.Is(err, apperr.ErrInvalid) {
		return httpx.InvalidParam("sellerId")
	}

	if err != nil {
		return err
	}

	if seller.Status != models.SellerStatusActive {
		return &apperr.Forbidden{Reason: "the seller is suspended"}
	}

	return nil
}

// checkItems validates the offered variants: they must be live variants of the product, each listed once, with a
// non-negative price and a discount between zero and the price.
func (s *Service) checkItems(ctx *gofr.Context, productID int64, items []models.SellerProductItem) error {
	if len(items) == 0 || len(items) > maxItems {
		return httpx.InvalidParam("items")
	}

	variants, err := s.variants.ListByProductIDs(ctx, []int64{productID})
	if err != nil {
		return httpx.FromStore(err, "items", "")
	}

	live := make(map[int64]bool, len(variants))
	for i := range variants {
		live[variants[i].ID] = true
	}

	seen := make(map[int64]bool, len(items))

	for _, it := range items {
		if !live[it.VariantID] || seen[it.VariantID] || it.MRP < 0 || it.Discount < 0 || it.Discount > it.MRP {
			return httpx.InvalidParam("items")
		}

		seen[it.VariantID] = true
	}

	return nil
}

// permitted returns 403 unless the seller is on the product's whitelist.
func (s *Service) permitted(ctx *gofr.Context, sellerID, productID int64) error {
	ok, err := s.permissible.Allowed(ctx, sellerID, productID)
	if err != nil {
		return httpx.FromStore(err, "sellerId", "")
	}

	if !ok {
		return &apperr.Forbidden{Reason: "the seller is not permitted to sell this product"}
	}

	return nil
}

// ownedBy returns apperr.ErrNotFound unless the caller may see request r. A seller sees only their own; admins and internal
// callers see all. It is "not found" rather than "forbidden" so a seller cannot probe which ids belong to other sellers.
func ownedBy(ctx *gofr.Context, r models.SellerProductRequest) error {
	if id, restricted := caller.Seller(ctx); restricted && r.SellerID != id {
		return apperr.ErrNotFound
	}

	return nil
}

// owned loads a request and checks the caller may see it.
func (s *Service) owned(ctx *gofr.Context, id int64) (models.SellerProductRequest, error) {
	r, err := s.requests.Get(ctx, id)
	if err == nil {
		err = ownedBy(ctx, r)
	}

	if err != nil {
		return models.SellerProductRequest{}, httpx.FromStoreID(err, "id", id)
	}

	return r, nil
}

// Create validates and files a request. A seller can only file for themselves.
func (s *Service) Create(ctx *gofr.Context, in *models.SellerProductRequestInput) (models.SellerProductRequest, error) {
	if id, restricted := caller.Seller(ctx); restricted && in.SellerID != id {
		return models.SellerProductRequest{}, &apperr.Forbidden{Reason: "you can only act for your own seller"}
	}

	if in.SellerID < 1 {
		return models.SellerProductRequest{}, httpx.InvalidParam("sellerId")
	}

	if in.ProductID < 1 {
		return models.SellerProductRequest{}, httpx.InvalidParam("productId")
	}

	if err := status(in); err != nil {
		return models.SellerProductRequest{}, err
	}

	if err := s.activeSeller(ctx, in.SellerID); err != nil {
		return models.SellerProductRequest{}, err
	}

	if _, err := s.products.Get(ctx, in.ProductID); err != nil {
		if errors.Is(err, apperr.ErrNotFound) {
			return models.SellerProductRequest{}, httpx.InvalidParam("productId")
		}

		return models.SellerProductRequest{}, err
	}

	if err := s.permitted(ctx, in.SellerID, in.ProductID); err != nil {
		return models.SellerProductRequest{}, err
	}

	if err := s.checkItems(ctx, in.ProductID, in.Items); err != nil {
		return models.SellerProductRequest{}, err
	}

	r, err := s.requests.Create(ctx, in)
	if err != nil {
		return models.SellerProductRequest{}, httpx.FromStore(err, "request", "")
	}

	return r, nil
}

// Get returns one request with its items, if the caller may see it.
func (s *Service) Get(ctx *gofr.Context, id int64) (models.SellerProductRequest, error) {
	return s.owned(ctx, id)
}

// List returns one page of requests.
func (s *Service) List(ctx *gofr.Context, f *filters.SellerProductRequestFilter) (models.SellerProductRequestList, error) {
	switch f.Status {
	case "", models.RequestDraft, models.RequestPending, models.RequestRejected, models.RequestApproved, models.RequestArchived:
	default:
		return models.SellerProductRequestList{}, httpx.InvalidParam("status")
	}

	if id, restricted := caller.Seller(ctx); restricted {
		f.SellerID = id // a seller lists only their own, whatever they asked for
	}

	requests, count, err := s.requests.List(ctx, f)
	if err != nil {
		return models.SellerProductRequestList{}, httpx.FromStore(err, "filter", "")
	}

	return models.SellerProductRequestList{Requests: requests, Count: count, Offset: f.Offset, Limit: f.Limit}, nil
}

// Update lets the seller revise a DRAFT or REJECTED request and resubmit it. The seller and product cannot change.
func (s *Service) Update(ctx *gofr.Context, id int64, in *models.SellerProductRequestInput) (models.SellerProductRequest, error) {
	if err := status(in); err != nil {
		return models.SellerProductRequest{}, err
	}

	current, err := s.owned(ctx, id)
	if err != nil {
		return models.SellerProductRequest{}, err
	}

	if err := s.activeSeller(ctx, current.SellerID); err != nil {
		return models.SellerProductRequest{}, err
	}

	if err := s.permitted(ctx, current.SellerID, current.ProductID); err != nil {
		return models.SellerProductRequest{}, err
	}

	if err := s.checkItems(ctx, current.ProductID, in.Items); err != nil {
		return models.SellerProductRequest{}, err
	}

	r, err := s.requests.Update(ctx, id, in)
	if err != nil {
		return models.SellerProductRequest{}, httpx.FromStoreID(err, "id", id)
	}

	r.SellerID, r.ProductID = current.SellerID, current.ProductID

	return r, nil
}

// Delete soft-deletes a request that is not APPROVED.
func (s *Service) Delete(ctx *gofr.Context, id int64) error {
	if _, err := s.owned(ctx, id); err != nil {
		return err
	}

	if err := s.requests.Delete(ctx, id); err != nil {
		return httpx.FromStoreID(err, "id", id)
	}

	return nil
}

// Approval approves or rejects a request. Approving a request that is already APPROVED changes nothing but re-sends the
// event, which is how a lost event is recovered.
func (s *Service) Approval(ctx *gofr.Context, id int64, req *models.ApprovalRequest) (models.SellerProductRequest, error) {
	if _, restricted := caller.Seller(ctx); restricted {
		return models.SellerProductRequest{}, &apperr.Forbidden{Reason: "only an administrator can approve or reject requests"}
	}

	req.Status = strings.ToUpper(strings.TrimSpace(req.Status))

	switch req.Status {
	case models.RequestRejected:
		reason := strings.TrimSpace(req.RejectionReason)
		if reason == "" {
			return models.SellerProductRequest{}, httpx.InvalidParam("rejectionReason")
		}

		r, err := s.requests.Reject(ctx, id, reason)
		if err != nil {
			return models.SellerProductRequest{}, httpx.FromStoreID(err, "id", id)
		}

		return r, nil
	case models.RequestApproved:
		return s.approve(ctx, id)
	default:
		return models.SellerProductRequest{}, httpx.InvalidParam("status")
	}
}

func (s *Service) approve(ctx *gofr.Context, id int64) (models.SellerProductRequest, error) {
	current, err := s.requests.Get(ctx, id)
	if err != nil {
		return models.SellerProductRequest{}, httpx.FromStoreID(err, "id", id)
	}

	if err := s.activeSeller(ctx, current.SellerID); err != nil {
		return models.SellerProductRequest{}, err
	}

	changed, err := s.requests.Approve(ctx, id)
	if err != nil {
		return models.SellerProductRequest{}, httpx.FromStoreID(err, "id", id)
	}

	if err := s.publish(ctx, changed); err != nil {
		return models.SellerProductRequest{}, err
	}

	return s.Get(ctx, id)
}

// Visibility shows or hides an approved request's live offer.
func (s *Service) Visibility(ctx *gofr.Context, id int64, req *models.VisibilityRequest) (models.SellerProductRequest, error) {
	req.ProductStatus = strings.ToUpper(strings.TrimSpace(req.ProductStatus))
	if !models.ValidStatus(req.ProductStatus) {
		return models.SellerProductRequest{}, httpx.InvalidParam("productStatus")
	}

	if _, err := s.owned(ctx, id); err != nil {
		return models.SellerProductRequest{}, err
	}

	r, err := s.requests.SetVisibility(ctx, id, req.ProductStatus)
	if err != nil {
		return models.SellerProductRequest{}, httpx.FromStoreID(err, "id", id)
	}

	if err := s.publish(ctx, []int64{id}); err != nil {
		return models.SellerProductRequest{}, err
	}

	return r, nil
}

func (s *Service) publish(ctx *gofr.Context, ids []int64) error {
	err := s.publisher.Publish(ctx, s.topic, models.SellerProductChangeEvent{SellerProductRequestIDs: ids})
	if err != nil {
		ctx.Errorf("cannot publish the seller product change event for requests %v: %v", ids, err)

		return ErrEventNotPublished
	}

	return nil
}
