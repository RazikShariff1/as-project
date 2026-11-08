package models

import "time"

// Variant is the sellable SKU. It carries content only; price, discount, tax and stock live in inventory-svc.
type Variant struct {
	ID           int64          `json:"id"`
	ProductID    int64          `json:"productId"`
	ClientItemID string         `json:"clientItemId"` // the seller's own item id (SKU)
	Name         string         `json:"name"`
	Status       string         `json:"status"`
	Barcodes     []string       `json:"barcodes"`
	Images       []string       `json:"images"`
	Videos       []string       `json:"videos"`
	MetaData     map[string]any `json:"metaData"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

// VariantRequest is the body for creating and updating a variant. ProductID is only used on create.
type VariantRequest struct {
	ProductID    int64          `json:"productId"`
	ClientItemID string         `json:"clientItemId"`
	Name         string         `json:"name"`
	Status       string         `json:"status"`
	Barcodes     []string       `json:"barcodes"`
	Images       []string       `json:"images"`
	Videos       []string       `json:"videos"`
	MetaData     map[string]any `json:"metaData"`
}

// VariantList is a page of variants with the total match count.
type VariantList struct {
	Variants []Variant `json:"variants"`
	Count    int       `json:"count"`
	Offset   int       `json:"offset"`
	Limit    int       `json:"limit"`
}
