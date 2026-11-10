package filters

import (
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
)

func TestOrderDetailsWhere(t *testing.T) {
	q, args := (&OrderDetails{}).Where()
	assert.Empty(t, q)
	assert.Empty(t, args)

	q, args = (&OrderDetails{CustomerID: 7, Statuses: []string{"PENDING", "PACKED"}, PaymentStatus: "PAID"}).Where()
	assert.Equal(t, " WHERE customer_id = $1 AND status = ANY($2) AND payment_status = $3", q)
	assert.Equal(t, []any{int64(7), pq.Array([]string{"PENDING", "PACKED"}), "PAID"}, args)
}

func TestOrderDetailsByReferenceNumbersAndPlacedDates(t *testing.T) {
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)

	q, args := (&OrderDetails{ReferenceNumbers: []int64{4, 9}, PlacedFrom: from, PlacedTo: to}).Where()

	assert.Equal(t, " WHERE id = ANY($1) AND created_at >= $2 AND created_at < $3", q)
	assert.Equal(t, []any{pq.Array([]int64{4, 9}), from, to.Add(24 * time.Hour)}, args)
}

func TestOrderDetailsPageNumbersPlaceholdersAfterTheWhereArgs(t *testing.T) {
	f := &OrderDetails{CustomerID: 7}
	_, args := f.Where()
	q, out := f.Page(args, 20, 40)

	assert.Equal(t, " ORDER BY id DESC LIMIT $2 OFFSET $3", q)
	assert.Equal(t, []any{int64(7), 20, 40}, out)
}

func TestOrderLogFilterWhere(t *testing.T) {
	q, args := (&OrderLog{}).Where()
	assert.Empty(t, q)
	assert.Empty(t, args)

	q, args = (&OrderLog{OrderReferenceNumber: "12", Action: "STATUS", CustomerID: 5}).Where()
	assert.Equal(t, " WHERE orders.id = $1::bigint AND order_logs.action = $2 AND orders.customer_id = $3", q)
	assert.Equal(t, []any{"12", "STATUS", int64(5)}, args)
}

func TestOrderFilterCompletedUpdatedAndSeller(t *testing.T) {
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	since := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

	q, args := (&OrderDetails{CompletedFrom: from, CompletedTo: to, UpdatedSince: since, SellerID: 7}).Where()

	assert.Equal(t, " WHERE completed_at >= $1 AND completed_at < $2 AND updated_at >= $3 AND "+
		"EXISTS (SELECT 1 FROM order_items oi WHERE oi.order_id = orders.id AND oi.seller_id = $4)", q)
	assert.Equal(t, []any{from, to.Add(24 * time.Hour), since, int64(7)}, args)
}
