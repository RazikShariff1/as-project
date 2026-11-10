// Package models holds the request and response structs (camelCase JSON, int64 ids).
package models

import (
	"encoding/json"
	"maps"
	"slices"
	"time"
)

// Order statuses, named as in the reference order-service. There are no in-store processing statuses (picking, checking):
// PENDING -> PACKED -> DISPATCHED -> COMPLETED, or CANCELLED from PENDING.
const (
	StatusPending    = "PENDING"
	StatusPacked     = "PACKED"
	StatusDispatched = "DISPATCHED"
	StatusCompleted  = "COMPLETED"
	StatusCancelled  = "CANCELLED"
)

// Payment statuses (as in the reference). payment-svc moves an order out of PENDING; a completed COD order becomes PAID by itself.
const (
	PaymentPending   = "PENDING"
	PaymentPaid      = "PAID"
	PaymentCancelled = "CANCELLED"
	PaymentRefund    = "REFUND"
)

// Order item statuses. A line removed from a pending order is CANCELLED and kept for the record.
const (
	ItemPending   = "PENDING"
	ItemCancelled = "CANCELLED"
)

// MethodCOD is cash on delivery, the only payment method so far.
const MethodCOD = "COD"

// Log actions written by order-svc itself. Staff may add entries with other actions through POST /order-log.
const (
	LogStatus  = "STATUS"
	LogPayment = "PAYMENT"
	LogItem    = "ITEM"
	LogUpdate  = "UPDATE"
)

// Address is where an order is delivered.
type Address struct {
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Line1   string `json:"line1"`
	Line2   string `json:"line2"`
	City    string `json:"city"`
	State   string `json:"state"`
	Pincode string `json:"pincode"`
	Country string `json:"country"`
}

// OrderItem is one seller's offer for a variant on an order, with the price frozen at order time. Quantity is in selling units.
type OrderItem struct {
	ID        int64   `json:"id"`
	VariantID int64   `json:"variantId"`
	SellerID  int64   `json:"sellerId"`
	Quantity  int64   `json:"quantity"`
	Unit      int     `json:"unit"` // base units per selling unit, frozen with the price
	Status    string  `json:"status"`
	MRP       float64 `json:"mrp"`
	Discount  float64 `json:"discount"`
	UnitPrice float64 `json:"unitPrice"`
	LineTotal float64 `json:"lineTotal"`
	Tax       string  `json:"tax"`
	HSNCode   string  `json:"hsnCode"`
}

// Order is a customer's order and everything that is saved with it in one transaction. ReferenceNumber is what callers use in paths
// (/order/{refNum}); OrderRef is the key of its stock reservation in inventory-svc and changes when items are edited.
type Order struct {
	ReferenceNumber int64          `json:"referenceNumber"`
	OrderRef        string         `json:"orderRef"`
	ClientID        string         `json:"clientId"`
	CustomerID      int64          `json:"customerId"`
	Status          string         `json:"status"`
	PaymentMethod   string         `json:"paymentMethod"`
	PaymentStatus   string         `json:"paymentStatus"`
	Subtotal        float64        `json:"subtotal"`
	DiscountTotal   float64        `json:"discountTotal"`
	Total           float64        `json:"total"`
	DeliveryAddress Address        `json:"deliveryAddress"`
	Notes           string         `json:"notes"`
	CancelReason    string         `json:"cancellationReason"`
	MetaData        map[string]any `json:"metaData"`
	Items           []OrderItem    `json:"items"`
	CreatedAt       time.Time      `json:"createdAt"`
	CompletedAt     *time.Time     `json:"completedAt"`
	// UpdatedAt doubles as the version of the order as it was read: the store refuses to save over a newer one.
	UpdatedAt time.Time `json:"updatedAt"`

	// StaleRefs are reservations an item edit replaced and that are still to be released.
	StaleRefs []string `json:"-"`
	// OrderLogs are history rows still to be written with the order (ID 0). The store saves them in the same transaction.
	OrderLogs []*OrderLog `json:"-"`
}

// DeepCopy returns a copy that can be changed without touching the original.
func (o *Order) DeepCopy() *Order {
	c := *o
	c.Items = slices.Clone(o.Items)
	c.MetaData = maps.Clone(o.MetaData)
	c.StaleRefs = slices.Clone(o.StaleRefs)
	c.OrderLogs = slices.Clone(o.OrderLogs)

	return &c
}

// Pagination is the page asked for.
type Pagination struct {
	Limit  int
	Offset int
}

// OrderList is a page of orders with the total match count, as {"count", "order": [...]}.
type OrderList struct {
	Count  int      `json:"count"`
	Offset int      `json:"offset"`
	Limit  int      `json:"limit"`
	Orders []*Order `json:"order"`
}

// OrderItemRequest asks for Quantity selling units of one seller's offer for a variant.
type OrderItemRequest struct {
	VariantID int64 `json:"variantId"`
	SellerID  int64 `json:"sellerId"`
	Quantity  int64 `json:"quantity"`
}

// OrderPlaceCreateHTTP is the body of POST /order. CustomerID is only honoured for staff and internal callers; a customer always
// orders for themselves.
//
// ClientID is the caller's own id for the order. Placing again with the same ClientID returns the order already placed instead of
// a second one, so a retried request is safe.
type OrderPlaceCreateHTTP struct {
	CustomerID      int64              `json:"customerId"`
	ClientID        string             `json:"clientId"`
	MetaData        map[string]any     `json:"metaData"`
	Items           []OrderItemRequest `json:"items"`
	DeliveryAddress Address            `json:"deliveryAddress"`
	PaymentMethod   string             `json:"paymentMethod"`
	Notes           string             `json:"notes"`
}

// OrderPlaceCreateSvcReq is what the place-order service works from: the HTTP body with the caller's identity resolved.
type OrderPlaceCreateSvcReq struct {
	CustomerID int64
	UserID     *int64
	ClientID   string
	MetaData   map[string]any
	Items      []OrderItemRequest
	Address    Address
	// PaymentMethod and Notes are as sent; the service normalises them.
	PaymentMethod string
	Notes         string
}

// OrderLog is one entry of an order's history, in the reference's shape: an action (STATUS, PAYMENT, ITEM, UPDATE or a staff entry
// such as NOTE) and its details. OrderID is the order's reference number.
type OrderLog struct {
	ID        int64           `json:"id"`
	OrderID   int64           `json:"orderId"`
	UserID    *int64          `json:"userId"`
	Action    string          `json:"action"`
	Details   json.RawMessage `json:"details"`
	CreatedAt time.Time       `json:"createdAt"`

	// ClientID is the order's own id from the caller, for OrderLogResponse.Order.
	ClientID string `json:"-"`
}

// OrderLogCreateRequest is the body of POST /order-log: a staff entry on an order.
type OrderLogCreateRequest struct {
	ReferenceNumber int64           `json:"referenceNumber"`
	Action          string          `json:"action"`
	Details         json.RawMessage `json:"details"`
}

// ExternalOrder carries the caller's own id of the order a history belongs to.
type ExternalOrder struct {
	ClientID string `json:"clientId"`
}

// OrderLogResponse is the answer to GET /order-log.
type OrderLogResponse struct {
	Limit    int            `json:"limit"`
	Offset   int            `json:"offset"`
	Count    int            `json:"count"`
	OrderLog []*OrderLog    `json:"orderlog"`
	Order    *ExternalOrder `json:"order"`
}

// Offer is what the order needs to know about a seller's offer, read from inventory-svc.
type Offer struct {
	VariantID    int64    `json:"variantId"`
	SellerID     int64    `json:"sellerId"`
	Unit         int      `json:"unit"`
	Status       string   `json:"status"`
	MRP          *float64 `json:"mrp"`
	Discount     float64  `json:"discount"`
	SellingPrice *float64 `json:"sellingPrice"`
	Tax          string   `json:"tax"`
	HSNCode      string   `json:"hsnCode"`
}

// ReservedItem is a quantity of one offer, in base units, held for an order.
type ReservedItem struct {
	VariantID int64 `json:"variantId"`
	SellerID  int64 `json:"sellerId"`
	Quantity  int64 `json:"quantity"`
}
