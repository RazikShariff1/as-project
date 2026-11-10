// Package utils holds helpers shared by the order services: pricing arithmetic, order references, request checks and history rows.
package utils

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/common/apperr"
	"github.com/zopping/order-svc/common/caller"
	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/models"
)

// Limits on what a request may carry.
const (
	MaxNotes     = 500
	MaxClientID  = 64
	MaxMetaBytes = 8 << 10
	MaxMetaKeys  = 50
	MaxAction    = 50

	centsPerRupee  = 100
	orderRefBytes  = 8
	orderRefPrefix = "ORD-"
)

// Round2 rounds an amount to 2 places.
func Round2(v float64) float64 { return math.Round(v*centsPerRupee) / centsPerRupee }

// NewOrderRef returns a unique reference for an order's stock reservation in inventory-svc.
func NewOrderRef() (string, error) {
	b := make([]byte, orderRefBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return orderRefPrefix + strings.ToUpper(hex.EncodeToString(b)), nil
}

// Totals sums an order's lines: subtotal at list price, the discount given, and what the customer pays. Removed lines do not count.
func Totals(items []models.OrderItem) (subtotal, discount, total float64) {
	for _, it := range items {
		if it.Status == models.ItemCancelled {
			continue
		}

		subtotal += it.MRP * float64(it.Quantity)
		discount += it.Discount * float64(it.Quantity)
		total += it.LineTotal
	}

	return Round2(subtotal), Round2(discount), Round2(total)
}

// NormaliseAddress trims an address and checks the parts a delivery cannot do without.
func NormaliseAddress(a *models.Address) error {
	for _, f := range []*string{&a.Name, &a.Phone, &a.Line1, &a.Line2, &a.City, &a.State, &a.Pincode, &a.Country} {
		*f = strings.TrimSpace(*f)
	}

	if a.Name == "" || a.Phone == "" || a.Line1 == "" || a.City == "" || a.Pincode == "" {
		return httpx.InvalidParam("deliveryAddress")
	}

	return nil
}

// CheckMetaData bounds free-form metadata: it is stored and returned with the order, so it must stay small.
func CheckMetaData(m map[string]any) error {
	if len(m) > MaxMetaKeys {
		return httpx.InvalidParam("metaData")
	}

	raw, err := json.Marshal(m)
	if err != nil || len(raw) > MaxMetaBytes {
		return httpx.InvalidParam("metaData")
	}

	return nil
}

// NewLog builds a history row (not yet saved) for an order whose id is not known yet or is orderID.
func NewLog(orderID int64, userID *int64, action string, details map[string]any) *models.OrderLog {
	raw, err := json.Marshal(details)
	if err != nil {
		raw = []byte("{}")
	}

	return &models.OrderLog{OrderID: orderID, UserID: userID, Action: action, Details: raw}
}

// Change is the details of a history row about one value changing from one thing to another.
func Change(from, to, note string) map[string]any {
	d := map[string]any{"from": from, "to": to}
	if note != "" {
		d["note"] = note
	}

	return d
}

// Visible reports whether the caller may see o: a customer only sees their own orders, staff and internal callers see all.
func Visible(ctx *gofr.Context, o *models.Order) bool {
	id, restricted := caller.Customer(ctx)

	return !restricted || o.CustomerID == id
}

// UserPtr returns the signed-in user the request is from, or nil for an internal call.
func UserPtr(ctx *gofr.Context) *int64 {
	if id, ok := caller.User(ctx); ok {
		return &id
	}

	return nil
}

// StaffOnly answers 403 to a customer.
func StaffOnly(ctx *gofr.Context) error {
	if _, restricted := caller.Customer(ctx); restricted {
		return &apperr.Forbidden{Reason: "only staff may do this"}
	}

	return nil
}
