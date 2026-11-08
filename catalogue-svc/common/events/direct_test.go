package events

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"

	"github.com/zopping/catalogue-svc/models"
)

func TestDirectPassesIDsToHandler(t *testing.T) {
	var got []int64

	d := NewDirect(func(_ *gofr.Context, ids []int64) error { got = ids; return nil })

	assert.NoError(t, d.Publish(&gofr.Context{}, "topic", models.SellerProductChangeEvent{SellerProductRequestIDs: []int64{4, 9}}))
	assert.Equal(t, []int64{4, 9}, got)
}

func TestDirectReturnsHandlerError(t *testing.T) {
	boom := errors.New("inventory-svc unreachable")
	d := NewDirect(func(*gofr.Context, []int64) error { return boom })

	assert.ErrorIs(t, d.Publish(&gofr.Context{}, "topic", models.SellerProductChangeEvent{}), boom)
}

func TestDirectRejectsOtherPayloads(t *testing.T) {
	d := NewDirect(func(*gofr.Context, []int64) error { return nil })

	assert.Error(t, d.Publish(&gofr.Context{}, "topic", "not an event"))
}
