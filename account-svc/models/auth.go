package models

import "time"

// LoginRequest exchanges credentials for a session token.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Session is returned once, at login. The token cannot be recovered later; only its hash is stored.
type Session struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	User      User      `json:"user"`
}

// Principal is who a valid token belongs to, as the gateway needs it. Attributes (e.g. sellerId) let the gateway
// scope a request to the caller's own data.
type Principal struct {
	User      User      `json:"user"`
	AllAccess bool      `json:"allAccess"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// SessionLookup is a live session joined to its user; used internally to authenticate a token.
type SessionLookup struct {
	SessionID int64
	ExpiresAt time.Time
	User      User
}
