package models

import "time"

// Seller statuses.
const (
	SellerStatusActive    = "active"
	SellerStatusSuspended = "suspended"
)

// Seller is a party whose products are sold on the storefront. The house seller is us.
type Seller struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	IsHouse   bool      `json:"isHouse"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SellerRequest is the body for creating (name, isHouse) and updating (name, status) a seller.
type SellerRequest struct {
	Name    string `json:"name"`
	IsHouse bool   `json:"isHouse"`
	Status  string `json:"status"`
}
