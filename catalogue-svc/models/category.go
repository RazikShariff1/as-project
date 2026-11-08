package models

import "time"

// Category is a node in the category tree; clients build the tree from ParentCategoryID.
type Category struct {
	ID               int64     `json:"id"`
	ParentCategoryID *int64    `json:"parentCategoryId"`
	Name             string    `json:"name"`
	Slug             string    `json:"slug"`
	Image            string    `json:"image"`
	Video            string    `json:"video"`
	Description      string    `json:"description"`
	TagLine          string    `json:"tagLine"`
	Status           string    `json:"status"`
	ClientID         string    `json:"clientId"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// CategoryRequest is the body for creating and updating a category. A blank slug is generated from the name.
type CategoryRequest struct {
	ParentCategoryID *int64 `json:"parentCategoryId"`
	Name             string `json:"name"`
	Slug             string `json:"slug"`
	Image            string `json:"image"`
	Video            string `json:"video"`
	Description      string `json:"description"`
	TagLine          string `json:"tagLine"`
	Status           string `json:"status"`
	ClientID         string `json:"clientId"`
}

// CategoryList is a page of categories with the total match count.
type CategoryList struct {
	Categories []Category `json:"categories"`
	Count      int        `json:"count"`
	Offset     int        `json:"offset"`
	Limit      int        `json:"limit"`
}
