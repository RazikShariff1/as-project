package models

import "time"

// Seller product request statuses.
const (
	RequestDraft    = "DRAFT"
	RequestPending  = "PENDING"
	RequestRejected = "REJECTED"
	RequestApproved = "APPROVED"
	RequestArchived = "ARCHIVED" // a previously approved request replaced by a newer approval
)

// SellerProductItem is one variant a request offers. MRP is the seller's list price and becomes the offer's mrp in
// inventory-svc (catalogue-service calls the same figure sellingPrice); the selling price is mrp minus discount.
type SellerProductItem struct {
	VariantID int64   `json:"variantId"`
	MRP       float64 `json:"mrp"`
	Discount  float64 `json:"discount"`
}

// SellerProductRequest is a seller's request to sell a shared product at their own price.
type SellerProductRequest struct {
	ID              int64               `json:"id"`
	SellerID        int64               `json:"sellerId"`
	ProductID       int64               `json:"productId"`
	Status          string              `json:"status"`
	RejectionReason string              `json:"rejectionReason"`
	ProductStatus   string              `json:"productStatus"` // ENABLED or HIDDEN: whether the live offer is visible
	Items           []SellerProductItem `json:"items"`
	SyncedAt        *time.Time          `json:"syncedAt"` // when the live state was last applied to inventory; null = never
	Synced          bool                `json:"synced"`   // false while an APPROVED or ARCHIVED request has a change inventory has not seen
	CreatedAt       time.Time           `json:"createdAt"`
	UpdatedAt       time.Time           `json:"updatedAt"`
}

// SellerProductRequestInput creates or replaces a request. Status is DRAFT or PENDING (default PENDING). On update
// only SellerID and ProductID are taken from the stored request and cannot change.
type SellerProductRequestInput struct {
	SellerID  int64               `json:"sellerId"`
	ProductID int64               `json:"productId"`
	Status    string              `json:"status"`
	Items     []SellerProductItem `json:"items"`
}

// ApprovalRequest approves or rejects a request. A rejection needs a reason.
type ApprovalRequest struct {
	Status          string `json:"status"`
	RejectionReason string `json:"rejectionReason"`
}

// VisibilityRequest shows or hides an approved request's live offer.
type VisibilityRequest struct {
	ProductStatus string `json:"productStatus"`
}

// SellerProductRequestList is a page of requests with the total match count.
type SellerProductRequestList struct {
	Requests []SellerProductRequest `json:"requests"`
	Count    int                    `json:"count"`
	Offset   int                    `json:"offset"`
	Limit    int                    `json:"limit"`
}

// SellerProductChangeEvent is published when a request's live state may have changed. Like catalogue-service's event it
// carries only ids: the subscriber reads the requests and makes inventory match their current state, so processing it
// twice, or out of order, ends in the same place.
type SellerProductChangeEvent struct {
	SellerProductRequestIDs []int64 `json:"sellerProductRequestIDs"`
}
