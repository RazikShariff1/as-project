// Package models holds the request and response structs (camelCase JSON, int64 ids).
package models

import "time"

// Order statuses.
const (
	StatusPlaced         = "PLACED"
	StatusConfirmed      = "CONFIRMED"
	StatusPacked         = "PACKED"
	StatusOutForDelivery = "OUT_FOR_DELIVERY"
	StatusDelivered      = "DELIVERED"
	StatusCancelled      = "CANCELLED"
)

// Payment statuses. payment-svc moves an order out of PENDING; a delivered COD order becomes PAID by itself.
const (
	PaymentPending  = "PENDING"
	PaymentPaid     = "PAID"
	PaymentFailed   = "FAILED"
	PaymentRefunded = "REFUNDED"
)

// MethodCOD is cash on delivery, the only payment method so far.
const MethodCOD = "COD"

// Log kinds.
const (
	LogStatus  = "STATUS"
	LogPayment = "PAYMENT"
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
	MRP       float64 `json:"mrp"`
	Discount  float64 `json:"discount"`
	UnitPrice float64 `json:"unitPrice"`
	LineTotal float64 `json:"lineTotal"`
	Tax       string  `json:"tax"`
	HSNCode   string  `json:"hsnCode"`
}

// Order is a customer's order.
type Order struct {
	ID              int64       `json:"id"`
	OrderRef        string      `json:"orderRef"`
	CustomerID      int64       `json:"customerId"`
	Status          string      `json:"status"`
	PaymentMethod   string      `json:"paymentMethod"`
	PaymentStatus   string      `json:"paymentStatus"`
	Subtotal        float64     `json:"subtotal"`
	DiscountTotal   float64     `json:"discountTotal"`
	Total           float64     `json:"total"`
	DeliveryAddress Address     `json:"deliveryAddress"`
	Notes           string      `json:"notes"`
	CancelReason    string      `json:"cancelReason"`
	Items           []OrderItem `json:"items"`
	CreatedAt       time.Time   `json:"createdAt"`
	UpdatedAt       time.Time   `json:"updatedAt"`
}

// OrderList is a page of orders (without items) with the total match count.
type OrderList struct {
	Orders []Order `json:"orders"`
	Count  int     `json:"count"`
	Offset int     `json:"offset"`
	Limit  int     `json:"limit"`
}

// OrderItemRequest asks for Quantity selling units of one seller's offer for a variant.
type OrderItemRequest struct {
	VariantID int64 `json:"variantId"`
	SellerID  int64 `json:"sellerId"`
	Quantity  int64 `json:"quantity"`
}

// OrderRequest places an order. CustomerID is only honoured for staff and internal callers; a customer always orders for themselves.
type OrderRequest struct {
	CustomerID      int64              `json:"customerId"`
	Items           []OrderItemRequest `json:"items"`
	DeliveryAddress Address            `json:"deliveryAddress"`
	PaymentMethod   string             `json:"paymentMethod"`
	Notes           string             `json:"notes"`
}

// StatusRequest moves an order to Status.
type StatusRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

// CancelRequest cancels an order.
type CancelRequest struct {
	Reason string `json:"reason"`
}

// PaymentStatusRequest records the payment outcome; payment-svc calls it.
type PaymentStatusRequest struct {
	PaymentStatus string `json:"paymentStatus"`
	Note          string `json:"note"`
}

// OrderLog is one entry of an order's history.
type OrderLog struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Note      string    `json:"note"`
	UserID    *int64    `json:"userId"`
	CreatedAt time.Time `json:"createdAt"`
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
