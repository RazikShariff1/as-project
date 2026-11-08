// Package models holds the request and response structs (camelCase JSON, int64 ids).
package models

import "time"

// Stock is the sellable quantity of one catalogue variant. Available = Quantity - Reserved.
type Stock struct {
	VariantID         int64     `json:"variantId"`
	SellerID          int64     `json:"sellerId"`
	Quantity          int64     `json:"quantity"`
	Reserved          int64     `json:"reserved"`
	Available         int64     `json:"available"`
	InStock           bool      `json:"inStock"`
	UnlimitedStock    bool      `json:"unlimitedStock"`
	Unit              int       `json:"unit"`
	Status            string    `json:"status"`
	LowStockThreshold *int64    `json:"lowStockThreshold"`
	MRP               *float64  `json:"mrp"`
	Discount          float64   `json:"discount"`
	CostPrice         *float64  `json:"costPrice"`
	SellingPrice      *float64  `json:"sellingPrice"` // mrp - discount; null until an mrp is set
	Tax               string    `json:"tax"`
	HSNCode           string    `json:"hsnCode"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// StockRequest sets one seller's stock of a variant to an absolute quantity. The offer must already exist: it is created by the
// catalogue's approval flow, never here. The variant and seller come from the path. Unit defaults to 1. Notes and UserID are
// recorded on the RECOUNTED movement. It never touches price fields or status: price has its own endpoint so a price edit can't
// overwrite a quantity that changed meanwhile, and status belongs to the catalogue (approval and visibility).
type StockRequest struct {
	Quantity          int64  `json:"quantity"`
	UnlimitedStock    bool   `json:"unlimitedStock"`
	Unit              int    `json:"unit"`
	LowStockThreshold *int64 `json:"lowStockThreshold"`
	Notes             string `json:"notes"`
	UserID            *int64 `json:"userId"`
}

// AdjustRequest changes a variant's quantity by Delta (positive receives stock, negative removes it).
// Reason must be one of the movement reasons (default OTHER). ReferenceType and ReferenceID (e.g. "GRN", "42")
// make the change idempotent: the same reference and reason is accepted once.
type AdjustRequest struct {
	Delta         int64  `json:"delta"`
	Reason        string `json:"reason"`
	Notes         string `json:"notes"`
	UserID        *int64 `json:"userId"`
	ReferenceType string `json:"referenceType"`
	ReferenceID   string `json:"referenceId"`
}

// Offer is one seller's visible offer for a variant, as the storefront sees it. It never includes cost price.
type Offer struct {
	SellerID       int64    `json:"sellerId"`
	Available      int64    `json:"available"`
	InStock        bool     `json:"inStock"`
	UnlimitedStock bool     `json:"unlimitedStock"`
	Unit           int      `json:"unit"`
	MRP            *float64 `json:"mrp"`
	Discount       float64  `json:"discount"`
	SellingPrice   *float64 `json:"sellingPrice"`
}

// VariantAvailability lists every visible seller's offer for a variant, cheapest first, and the one to show by default:
// the lowest selling price among sellers with stock. Hidden offers are left out. Offers is empty when nobody sells it.
type VariantAvailability struct {
	VariantID int64   `json:"variantId"`
	Offers    []Offer `json:"offers"`
	BestOffer *Offer  `json:"bestOffer"`
}

// OfferRequest creates or updates a seller's offer when a seller product request is approved. It sets the MRP and
// discount and makes the offer visible, and never touches quantity, so repeating it is safe.
type OfferRequest struct {
	MRP      float64 `json:"mrp"`
	Discount float64 `json:"discount"`
	Notes    string  `json:"notes"`
	UserID   *int64  `json:"userId"`
}

// PriceRequest replaces a variant's commercial details. MRP is required whenever Discount is above zero. Amounts are
// in rupees; Discount is an amount off the MRP, not a percentage.
type PriceRequest struct {
	MRP       *float64 `json:"mrp"`
	Discount  float64  `json:"discount"`
	CostPrice *float64 `json:"costPrice"`
	Tax       string   `json:"tax"`
	HSNCode   string   `json:"hsnCode"`
	Notes     string   `json:"notes"`
	UserID    *int64   `json:"userId"`
}

// Price fields recorded in the price log.
const (
	PriceFieldMRP       = "MRP"
	PriceFieldDiscount  = "DISCOUNT"
	PriceFieldCostPrice = "COST_PRICE"
	PriceFieldTax       = "TAX"
	PriceFieldHSNCode   = "HSN_CODE"
)

// PriceChange is one audited change to a price field. OldValue is null for a field's first value.
type PriceChange struct {
	ID        int64     `json:"id"`
	VariantID int64     `json:"variantId"`
	SellerID  int64     `json:"sellerId"`
	Field     string    `json:"field"`
	OldValue  *string   `json:"oldValue"`
	NewValue  *string   `json:"newValue"`
	Notes     string    `json:"notes"`
	UserID    *int64    `json:"userId"`
	CreatedAt time.Time `json:"createdAt"`
}

// PriceChangeList is a page of price changes, newest first, with the total match count.
type PriceChangeList struct {
	Changes []PriceChange `json:"changes"`
	Count   int           `json:"count"`
	Offset  int           `json:"offset"`
	Limit   int           `json:"limit"`
}

// Movement is one audited quantity change.
type Movement struct {
	ID            int64     `json:"id"`
	VariantID     int64     `json:"variantId"`
	SellerID      int64     `json:"sellerId"`
	Delta         int64     `json:"delta"`
	QuantityAfter int64     `json:"quantityAfter"`
	Reason        string    `json:"reason"`
	Notes         string    `json:"notes"`
	UserID        *int64    `json:"userId"`
	ReferenceType string    `json:"referenceType"`
	ReferenceID   string    `json:"referenceId"`
	CreatedAt     time.Time `json:"createdAt"`
}

// StockList is a page of stock rows with the total match count.
type StockList struct {
	Stock  []Stock `json:"stock"`
	Count  int     `json:"count"`
	Offset int     `json:"offset"`
	Limit  int     `json:"limit"`
}

// MovementList is a page of movements, newest first, with the total match count.
type MovementList struct {
	Movements []Movement `json:"movements"`
	Count     int        `json:"count"`
	Offset    int        `json:"offset"`
	Limit     int        `json:"limit"`
}

// Movement reasons, as in inventory-service minus its distribution-centre ones (a marketplace has sellers, not DCs).
const (
	ReasonReceived         = "RECEIVED"
	ReasonReturnedToSeller = "RETURNED_TO_SELLER"
	ReasonExpired          = "EXPIRED"
	ReasonDamaged          = "DAMAGED"
	ReasonReturned         = "RETURNED" // a customer return
	ReasonSold             = "SOLD"
	ReasonRecounted        = "RECOUNTED"
	ReasonOther            = "OTHER"
)

// ReferenceOrder is the reference type written on sale movements.
const ReferenceOrder = "ORDER"

// Statuses of a stock row.
const (
	StatusEnabled = "ENABLED"
	StatusHidden  = "HIDDEN"
)

// ValidReason reports whether r is a known movement reason.
func ValidReason(r string) bool {
	switch r {
	case ReasonReceived, ReasonReturnedToSeller, ReasonExpired, ReasonDamaged, ReasonReturned, ReasonSold,
		ReasonRecounted, ReasonOther:
		return true
	default:
		return false
	}
}

// ValidStatus reports whether s is ENABLED or HIDDEN.
func ValidStatus(s string) bool { return s == StatusEnabled || s == StatusHidden }
