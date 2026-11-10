package password

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashAndCompare(t *testing.T) {
	h := New(4)

	hash, err := h.Hash("correct horse")
	assert.NoError(t, err)
	assert.NotContains(t, hash, "correct horse", "the hash must not contain the password")

	other, _ := h.Hash("correct horse")
	assert.NotEqual(t, hash, other, "each hash has its own salt")

	assert.NoError(t, h.Compare(hash, "correct horse"))
	assert.ErrorIs(t, h.Compare(hash, "wrong horse"), ErrMismatch)
	assert.Error(t, h.Compare("not-a-bcrypt-hash", "x"))
	assert.NotErrorIs(t, h.Compare("not-a-bcrypt-hash", "x"), ErrMismatch, "a corrupt hash is an error, not a mismatch")
}

func TestValid(t *testing.T) {
	assert.False(t, Valid("short"))
	assert.True(t, Valid("12345678"))
	assert.True(t, Valid(strings.Repeat("a", 72)))
	assert.False(t, Valid(strings.Repeat("a", 73)), "bcrypt would silently ignore everything past 72 bytes")
}
