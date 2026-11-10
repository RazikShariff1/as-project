package placeorder

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/apperr"
	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/utils"
)

const (
	maxItems       = 100
	maxParallel    = 8 // offers read from inventory-svc at the same time
	statusEnabled  = "ENABLED"
	minCustomerID  = 1
	minItemQuantum = 1
)

// offerKey identifies one seller's offer for a variant.
type offerKey struct{ variantID, sellerID int64 }

// validateOrderCreate checks the request and normalises it in place: repeated offers become one line (sorted by variant, then
// seller), text is trimmed and the payment method defaulted.
func (*service) validateOrderCreate(req *models.OrderPlaceCreateSvcReq) error {
	if isCartEmpty(req) {
		return httpx.InvalidParam("items")
	}

	if req.CustomerID < minCustomerID {
		return httpx.InvalidParam("customerId") // staff and internal callers must say who the order is for
	}

	if err := mergeItems(req); err != nil {
		return err
	}

	if err := utils.NormaliseAddress(&req.Address); err != nil {
		return err
	}

	if err := paymentValidation(req); err != nil {
		return err
	}

	req.Notes = strings.TrimSpace(req.Notes)
	if len(req.Notes) > utils.MaxNotes {
		return httpx.InvalidParam("notes")
	}

	req.ClientID = strings.TrimSpace(req.ClientID)
	if len(req.ClientID) > utils.MaxClientID {
		return httpx.InvalidParam("clientId")
	}

	if err := utils.CheckMetaData(req.MetaData); err != nil {
		return err
	}

	if req.MetaData == nil {
		req.MetaData = map[string]any{}
	}

	return nil
}

func isCartEmpty(req *models.OrderPlaceCreateSvcReq) bool {
	return len(req.Items) == 0 || len(req.Items) > maxItems
}

// mergeItems validates each line and merges repeated offers into one.
func mergeItems(req *models.OrderPlaceCreateSvcReq) error {
	merged := make(map[offerKey]int64, len(req.Items))

	for _, it := range req.Items {
		if it.VariantID < 1 || it.SellerID < 1 || it.Quantity < minItemQuantum {
			return httpx.InvalidParam("items")
		}

		merged[offerKey{it.VariantID, it.SellerID}] += it.Quantity
	}

	req.Items = req.Items[:0]
	for k, q := range merged {
		req.Items = append(req.Items, models.OrderItemRequest{VariantID: k.variantID, SellerID: k.sellerID, Quantity: q})
	}

	sort.Slice(req.Items, func(i, j int) bool {
		if req.Items[i].VariantID != req.Items[j].VariantID {
			return req.Items[i].VariantID < req.Items[j].VariantID
		}

		return req.Items[i].SellerID < req.Items[j].SellerID
	})

	return nil
}

// paymentValidation defaults the payment method and checks it is one we take: cash on delivery until payment-svc exists.
func paymentValidation(req *models.OrderPlaceCreateSvcReq) error {
	req.PaymentMethod = strings.ToUpper(strings.TrimSpace(req.PaymentMethod))
	if req.PaymentMethod == "" {
		req.PaymentMethod = models.MethodCOD
	}

	if req.PaymentMethod != models.MethodCOD {
		return httpx.InvalidParam("paymentMethod")
	}

	return nil
}

// processOrderItems reads every offer from inventory-svc at once (at most maxParallel in flight) and freezes each price on an order
// line. It also returns what to reserve: quantity is in selling units, and stock is held in base units (quantity x unit). The first
// failing item, in request order, decides the error.
func (s *service) processOrderItems(ctx *gofr.Context, req *models.OrderPlaceCreateSvcReq) ([]models.OrderItem, []models.ReservedItem, error) {
	var (
		offers = make([]models.Offer, len(req.Items))
		errs   = make([]error, len(req.Items))
		wg     sync.WaitGroup
		slots  = make(chan struct{}, maxParallel)
	)

	for i, it := range req.Items {
		slots <- struct{}{}

		wg.Add(1)

		go func() {
			defer wg.Done()
			defer func() { <-slots }()

			offers[i], errs[i] = s.inventory.Offer(ctx, it.VariantID, it.SellerID)
		}()
	}

	wg.Wait()

	items := make([]models.OrderItem, 0, len(req.Items))
	reserve := make([]models.ReservedItem, 0, len(req.Items))

	for i, it := range req.Items {
		if errs[i] != nil {
			return nil, nil, offerError(errs[i], it)
		}

		offer := offers[i]
		if offer.Status != statusEnabled || offer.SellingPrice == nil || offer.MRP == nil || offer.Unit < 1 {
			return nil, nil, unavailable(it)
		}

		items = append(items, models.OrderItem{
			VariantID: it.VariantID, SellerID: it.SellerID, Quantity: it.Quantity, Unit: offer.Unit, Status: models.ItemPending,
			MRP: *offer.MRP, Discount: offer.Discount, UnitPrice: *offer.SellingPrice,
			LineTotal: utils.Round2(*offer.SellingPrice * float64(it.Quantity)), Tax: offer.Tax, HSNCode: offer.HSNCode,
		})
		reserve = append(reserve, models.ReservedItem{VariantID: it.VariantID, SellerID: it.SellerID, Quantity: it.Quantity * int64(offer.Unit)})
	}

	return items, reserve, nil
}

func offerError(err error, it models.OrderItemRequest) error {
	if isNotFound(err) {
		return unavailable(it)
	}

	return err
}

func isNotFound(err error) bool { return errors.Is(err, apperr.ErrNotFound) }

func unavailable(it models.OrderItemRequest) error {
	return httpx.Conflict(fmt.Sprintf("variant %d from seller %d is not available to order", it.VariantID, it.SellerID))
}
