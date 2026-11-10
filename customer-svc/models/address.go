package models

import "time"

// Address is a saved delivery address of one customer.
type Address struct {
	ID         int64     `json:"id"`
	CustomerID int64     `json:"customerId"`
	Label      string    `json:"label"`
	Recipient  string    `json:"recipient"`
	Phone      string    `json:"phone"`
	Line1      string    `json:"line1"`
	Line2      string    `json:"line2"`
	City       string    `json:"city"`
	State      string    `json:"state"`
	PostalCode string    `json:"postalCode"`
	Country    string    `json:"country"`
	IsDefault  bool      `json:"isDefault"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// AddressRequest creates or replaces an address. The first address a customer saves becomes their default.
type AddressRequest struct {
	Label      string `json:"label"`
	Recipient  string `json:"recipient"`
	Phone      string `json:"phone"`
	Line1      string `json:"line1"`
	Line2      string `json:"line2"`
	City       string `json:"city"`
	State      string `json:"state"`
	PostalCode string `json:"postalCode"`
	Country    string `json:"country"`
	IsDefault  bool   `json:"isDefault"`
}
