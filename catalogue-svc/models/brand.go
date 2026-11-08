package models

import "time"

// Brand is a product brand with its own storefront page content.
type Brand struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Logo        string    `json:"logo"`
	Image       string    `json:"image"`
	Video       string    `json:"video"`
	Description string    `json:"description"`
	TagLine     string    `json:"tagLine"`
	Status      string    `json:"status"`
	ClientID    string    `json:"clientId"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// BrandRequest is the body for creating and updating a brand. A blank slug is generated from the name.
type BrandRequest struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Logo        string `json:"logo"`
	Image       string `json:"image"`
	Video       string `json:"video"`
	Description string `json:"description"`
	TagLine     string `json:"tagLine"`
	Status      string `json:"status"`
	ClientID    string `json:"clientId"`
}

// BrandList is a page of brands with the total match count.
type BrandList struct {
	Brands []Brand `json:"brands"`
	Count  int     `json:"count"`
	Offset int     `json:"offset"`
	Limit  int     `json:"limit"`
}
