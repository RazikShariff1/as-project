package order

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/common/testutil"
	"github.com/zopping/order-svc/filters"
)

func TestOrderFilterReadsEveryFilter(t *testing.T) {
	ctx := testutil.CtxWithQuery(t, testutil.Staff, "id=9&referenceNumber=4,5&status=pending,PACKED&paymentStatus=paid&customerId=7&sellerID=3"+
		"&placedBetween=2026-11-01,2026-11-03&completedBetween=2026-11-02,2026-11-02&updatedSince=2026-11-02T10:00:00Z")

	got, err := orderFilter(ctx)

	assert.NoError(t, err)
	assert.Equal(t, &filters.OrderDetails{
		CustomerID: 7, SellerID: 3, Statuses: []string{"PENDING", "PACKED"}, ReferenceNumbers: []int64{4, 5, 9}, PaymentStatus: "PAID",
		PlacedFrom: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), PlacedTo: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
		CompletedFrom: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), CompletedTo: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
		UpdatedSince: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC),
	}, got)
}

func TestOrderFilterRejectsWhatItCannotRead(t *testing.T) {
	for _, q := range []string{
		"status=PICKING", "status=PENDING,NOPE", "referenceNumber=1,x", "referenceNumber=0", "id=abc", "paymentStatus=maybe", "customerId=-1",
		"sellerID=x", "placedBetween=2026-11-01", "placedBetween=2026-11-05,2026-11-01", "placedBetween=nope,2026-11-01",
		"completedBetween=2026-11-05,2026-11-01", "updatedSince=yesterday",
	} {
		_, err := orderFilter(testutil.CtxWithQuery(t, testutil.Staff, q))
		assert.Error(t, err, q)
	}
}

func TestOrderFilterWithNothingMatchesEverything(t *testing.T) {
	got, err := orderFilter(testutil.CtxAs(t, testutil.Staff))

	assert.NoError(t, err)

	where, args := got.Where()
	assert.Empty(t, where)
	assert.Empty(t, args)
}

func TestUpdatedSinceAcceptsADateOrADateTimeAsUTC(t *testing.T) {
	for raw, want := range map[string]time.Time{
		"2026-11-02":                time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
		"2026-11-02 10:30:00":       time.Date(2026, 11, 2, 10, 30, 0, 0, time.UTC),
		"2026-11-02T10:30:00":       time.Date(2026, 11, 2, 10, 30, 0, 0, time.UTC),
		"2026-11-02T16:00:00+05:30": time.Date(2026, 11, 2, 10, 30, 0, 0, time.UTC),
	} {
		got, err := getUpdatedSince(raw)
		assert.NoError(t, err, raw)
		assert.True(t, want.Equal(got), raw)
	}
}

func TestPaginationFollowsTheReference(t *testing.T) {
	for name, tc := range map[string]struct {
		query         string
		limit, offset int
		bad           bool
	}{
		"defaults":                {"", 20, 0, false},
		"limit and offset":        {"limit=10&offset=30", 10, 30, false},
		"offset alone":            {"offset=5", 20, 5, false},
		"legacy page and perPage": {"page=3&perPage=10", 10, 20, false},
		"limit wins over legacy":  {"limit=5&page=3&perPage=10", 5, 0, false},
		"unpaginated":             {"paginated=false", httpx.MaxUnpaginated, 0, false},
		"paginated explicitly":    {"paginated=true&limit=7", 7, 0, false},
		"too large":               {"limit=101", 0, 0, true},
		"bad offset":              {"offset=-1", 0, 0, true},
		"bad page":                {"page=0", 0, 0, true},
		"bad perPage":             {"page=1&perPage=0", 0, 0, true},
		"bad paginated":           {"paginated=maybe", 0, 0, true},
	} {
		limit, offset, err := httpx.Paginate(testutil.CtxWithQuery(t, testutil.Staff, tc.query))

		if tc.bad {
			assert.Error(t, err, name)

			continue
		}

		assert.NoError(t, err, name)
		assert.Equal(t, tc.limit, limit, name)
		assert.Equal(t, tc.offset, offset, name)
	}
}
