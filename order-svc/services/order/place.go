package order

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/caller"
	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/models"
)

const (
	maxItems       = 100
	maxNotes       = 500
	maxTTLSeconds  = 24 * 60 * 60
	orderRefBytes  = 8
	centsPerRupee  = 100
	statusEnabled  = "ENABLED"
	orderRefPrefix = "ORD-"
)

// offerKey identifies one seller's offer for a variant.
type offerKey struct{ variantID, sellerID int64 }

func round2(v float64) float64 { return math.Round(v*centsPerRupee) / centsPerRupee }

// newOrderRef returns a unique reference for an order, used with inventory-svc's reservations.
func newOrderRef() (string, error) {
	b := make([]byte, orderRefBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return orderRefPrefix + strings.ToUpper(hex.EncodeToString(b)), nil
}

// customerFor decides who the order is for: a signed-in customer always orders for themselves; staff and internal callers name
// the customer in the request.
func customerFor(ctx *gofr.Context, req *models.OrderRequest) (int64, error) {
	if id, restricted := caller.Customer(ctx); restricted {
		return id, nil
	}

	if req.CustomerID < 1 {
		return 0, httpx.InvalidParam("customerId")
	}

	return req.CustomerID, nil
}

// normalise validates the request and merges repeated offers into one line, sorted by variant then seller.
func normalise(req *models.OrderRequest) error {
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
		req.Items = append(req.Items, models.OrderItemRequest{VariantID: k.variantID, SellerID: k.sellerID, Quantity: q})
	}

	sort.Slice(req.Items, func(i, j int) bool {
		if req.Items[i].VariantID != req.Items[j].VariantID {
			return req.Items[i].VariantID < req.Items[j].VariantID
		}

		return req.Items[i].SellerID < req.Items[j].SellerID
	})

	return normaliseDetails(req)
}

func normaliseDetails(req *models.OrderRequest) error {
	a := &req.DeliveryAddress
	for _, f := range []*string{&a.Name, &a.Phone, &a.Line1, &a.Line2, &a.City, &a.State, &a.Pincode, &a.Country} {
		*f = strings.TrimSpace(*f)
	}

	if a.Name == "" || a.Phone == "" || a.Line1 == "" || a.City == "" || a.Pincode == "" {
		return httpx.InvalidParam("deliveryAddress")
	}

	req.PaymentMethod = strings.ToUpper(strings.TrimSpace(req.PaymentMethod))
	if req.PaymentMethod == "" {
		req.PaymentMethod = models.MethodCOD
	}

	if req.PaymentMethod != models.MethodCOD {
		return httpx.InvalidParam("paymentMethod")
	}

	req.Notes = strings.TrimSpace(req.Notes)
	if len(req.Notes) > maxNotes {
		return httpx.InvalidParam("notes")
	}

	return nil
}

// priceItems reads each offer from inventory-svc and freezes its price on an order line. It also returns what to reserve: quantity
// is in selling units, and stock is held in base units (quantity x unit).
func (s *Service) priceItems(ctx *gofr.Context, req *models.OrderRequest) ([]models.OrderItem, []models.ReservedItem, error) {
	items := make([]models.OrderItem, 0, len(req.Items))
	reserve := make([]models.ReservedItem, 0, len(req.Items))

	for _, it := range req.Items {
		offer, err := s.inventory.Offer(ctx, it.VariantID, it.SellerID)
		if err != nil {
			return nil, nil, offerError(err, it)
		}

		if offer.Status != statusEnabled || offer.SellingPrice == nil || offer.MRP == nil || offer.Unit < 1 {
			return nil, nil, unavailable(it)
		}

		items = append(items, models.OrderItem{
			VariantID: it.VariantID, SellerID: it.SellerID, Quantity: it.Quantity,
			MRP: *offer.MRP, Discount: offer.Discount, UnitPrice: *offer.SellingPrice,
			LineTotal: round2(*offer.SellingPrice * float64(it.Quantity)), Tax: offer.Tax, HSNCode: offer.HSNCode,
		})
		reserve = append(reserve, models.ReservedItem{VariantID: it.VariantID, SellerID: it.SellerID, Quantity: it.Quantity * int64(offer.Unit)})
	}

	return items, reserve, nil
}

func unavailable(it models.OrderItemRequest) error {
	return httpx.Conflict(fmt.Sprintf("variant %d from seller %d is not available to order", it.VariantID, it.SellerID))
}

// totals sums an order's lines: subtotal at list price, the discount given, and what the customer pays.
func totals(items []models.OrderItem) (subtotal, discount, total float64) {
	for _, it := range items {
		subtotal += it.MRP * float64(it.Quantity)
		discount += it.Discount * float64(it.Quantity)
		total += it.LineTotal
	}

	return round2(subtotal), round2(discount), round2(total)
}
