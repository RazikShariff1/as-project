package filters

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOrderFilterWhere(t *testing.T) {
	q, args := (&OrderFilter{}).Where()
	assert.Empty(t, q)
	assert.Empty(t, args)

	q, args = (&OrderFilter{CustomerID: 7, Status: "PLACED"}).Where()
	assert.Equal(t, " WHERE customer_id = $1 AND status = $2", q)
	assert.Equal(t, []any{int64(7), "PLACED"}, args)
}

func TestOrderFilterPageNumbersPlaceholdersAfterTheWhereArgs(t *testing.T) {
	f := &OrderFilter{CustomerID: 7, Limit: 20, Offset: 40}
	_, args := f.Where()
	q, out := f.Page(args)

	assert.Equal(t, " ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3", q)
	assert.Equal(t, []any{int64(7), 20, 40}, out)
}
