package slug

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMake(t *testing.T) {
	tests := map[string]string{
		"Green Tea":          "green-tea",
		"  Tea & Coffee!! ":  "tea-coffee",
		"Fresh---Milk 500ml": "fresh-milk-500ml",
		"!!!":                "",
		"":                   "",
		"already-a-slug":     "already-a-slug",
	}

	for in, want := range tests {
		assert.Equal(t, want, Make(in), in)
	}
}

func TestValidAndResolve(t *testing.T) {
	assert.True(t, Valid("green-tea"))
	assert.False(t, Valid("Green Tea"))
	assert.False(t, Valid("-leading"))
	assert.False(t, Valid("double--hyphen"))
	assert.False(t, Valid(""))

	assert.Equal(t, "custom", Resolve(" custom ", "Ignored Name"))
	assert.Equal(t, "ignored-name", Resolve("", "Ignored Name"))
}
