// Package password hashes and checks passwords with bcrypt.
package password

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

const (
	// MinLength and MaxLength bound a password. bcrypt ignores everything past 72 bytes, so longer is rejected
	// rather than silently truncated.
	MinLength = 8
	MaxLength = 72

	// DefaultCost is bcrypt's work factor for production use.
	DefaultCost = 12
)

// ErrMismatch is returned by Compare when the password does not match the hash.
var ErrMismatch = errors.New("password does not match")

// Hasher hashes and checks passwords.
type Hasher struct {
	cost int
}

// New returns a Hasher with the given bcrypt cost (use DefaultCost outside tests).
func New(cost int) *Hasher { return &Hasher{cost: cost} }

// Valid reports whether a password has an acceptable length.
func Valid(pw string) bool { return len(pw) >= MinLength && len(pw) <= MaxLength }

// Hash returns the bcrypt hash of pw.
func (h *Hasher) Hash(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), h.cost)

	return string(b), err
}

// Compare returns nil when pw matches hash, ErrMismatch when it does not, and any other error unchanged.
func (*Hasher) Compare(hash, pw string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return ErrMismatch
	}

	return err
}
