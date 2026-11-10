// Package models holds the request and response structs (camelCase JSON, int64 ids).
package models

import "time"

// Customer statuses.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

// ValidStatus reports whether s is a known customer status.
func ValidStatus(s string) bool { return s == StatusActive || s == StatusSuspended }

// Customer is a shopper. The password hash is never part of it.
type Customer struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Email       string     `json:"email"`
	Phone       string     `json:"phone"`
	Status      string     `json:"status"`
	LastLoginAt *time.Time `json:"lastLoginAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// RegisterRequest creates a customer account.
type RegisterRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

// ProfileRequest replaces the fields a customer may change about themselves.
type ProfileRequest struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

// StatusRequest suspends or reactivates a customer (staff).
type StatusRequest struct {
	Status string `json:"status"`
}

// PasswordChangeRequest lets a customer change their own password; the current one must be right.
type PasswordChangeRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// CustomerList is a page of customers with the total match count.
type CustomerList struct {
	Customers []Customer `json:"customers"`
	Count     int        `json:"count"`
	Offset    int        `json:"offset"`
	Limit     int        `json:"limit"`
}

// Credentials is what login needs about a customer. It never leaves the service.
type Credentials struct {
	Customer     Customer
	PasswordHash string
	FailedLogins int
	LockedUntil  *time.Time
}

// LoginRequest exchanges credentials for a session token.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Session is returned once, at login or registration. The token cannot be recovered later; only its hash is stored.
type Session struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	Customer  Customer  `json:"customer"`
}

// Principal is who a valid token belongs to, as the gateway needs it.
type Principal struct {
	Customer  Customer  `json:"customer"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// SessionLookup is a live session joined to its customer.
type SessionLookup struct {
	SessionID int64
	ExpiresAt time.Time
	Customer  Customer
}
