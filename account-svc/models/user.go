// Package models holds the request and response structs (camelCase JSON, int64 ids).
package models

import "time"

// User statuses.
const (
	UserStatusActive    = "active"
	UserStatusSuspended = "suspended"
)

// ValidUserStatus reports whether s is a known user status.
func ValidUserStatus(s string) bool { return s == UserStatusActive || s == UserStatusSuspended }

// User is an account. The password hash is never part of it.
type User struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Email       string         `json:"email"`
	Phone       string         `json:"phone"`
	Status      string         `json:"status"`
	Attributes  map[string]any `json:"attributes"`
	RoleIDs     []int64        `json:"roleIds"`
	LastLoginAt *time.Time     `json:"lastLoginAt"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

// UserRequest creates or replaces a user. Password is only used on create (use the password endpoints afterwards).
// Status defaults to active. RoleIDs replaces the user's roles.
type UserRequest struct {
	Name       string         `json:"name"`
	Email      string         `json:"email"`
	Phone      string         `json:"phone"`
	Password   string         `json:"password"`
	Status     string         `json:"status"`
	Attributes map[string]any `json:"attributes"`
	RoleIDs    []int64        `json:"roleIds"`
}

// UserList is a page of users with the total match count.
type UserList struct {
	Users  []User `json:"users"`
	Count  int    `json:"count"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

// Credentials is what login needs about a user. It never leaves the service.
type Credentials struct {
	User         User
	PasswordHash string
	FailedLogins int
	LockedUntil  *time.Time
}

// PasswordChangeRequest lets a user change their own password; the current one must be right.
type PasswordChangeRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// PasswordResetRequest sets a user's password without knowing the old one; guard it with its own permission.
type PasswordResetRequest struct {
	NewPassword string `json:"newPassword"`
}
