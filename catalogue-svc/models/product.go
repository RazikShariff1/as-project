package models

import "time"

// Product is a shared catalogue listing: content only, sold through one or more variants. Sellers do not own products;
// each seller offers a variant with their own stock and price in inventory-svc.
type Product struct {
	ID                int64          `json:"id"`
	PrimaryCategoryID *int64         `json:"primaryCategoryId"`
	CategoryIDs       []int64        `json:"categoryIds"` // every category the product is listed in
	BrandID           *int64         `json:"brandId"`
	Name              string         `json:"name"`
	Slug              string         `json:"slug"`
	TagLine           string         `json:"tagLine"`
	Description       string         `json:"description"`
	Images            []string       `json:"images"`
	Videos            []string       `json:"videos"`
	Status            string         `json:"status"`
	HasVariants       bool           `json:"hasVariants"`
	SoldByWeight      bool           `json:"soldByWeight"`
	MetaData          map[string]any `json:"metaData"`
	ClientItemID      string         `json:"clientItemId"`
	Barcodes          []string       `json:"barcodes"`
	Variants          []Variant      `json:"variants"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
}

// ProductRequest is the body for creating and updating a product.
// A blank slug is generated from the name. CategoryIDs replaces the product's extra categories.
type ProductRequest struct {
	PrimaryCategoryID *int64         `json:"primaryCategoryId"`
	CategoryIDs       []int64        `json:"categoryIds"`
	BrandID           *int64         `json:"brandId"`
	Name              string         `json:"name"`
	Slug              string         `json:"slug"`
	TagLine           string         `json:"tagLine"`
	Description       string         `json:"description"`
	Images            []string       `json:"images"`
	Videos            []string       `json:"videos"`
	Status            string         `json:"status"`
	HasVariants       bool           `json:"hasVariants"`
	SoldByWeight      bool           `json:"soldByWeight"`
	MetaData          map[string]any `json:"metaData"`
	ClientItemID      string         `json:"clientItemId"`
	Barcodes          []string       `json:"barcodes"`
}

// ProductList is a page of products with the total match count.
type ProductList struct {
	Products []Product `json:"products"`
	Count    int       `json:"count"`
	Offset   int       `json:"offset"`
	Limit    int       `json:"limit"`
}
