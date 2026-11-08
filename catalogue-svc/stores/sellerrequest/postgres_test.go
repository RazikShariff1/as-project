package sellerrequest

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/zopping/catalogue-svc/models"
)

func TestInSync(t *testing.T) {
	changed := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	stamp := func(offset time.Duration) sql.NullTime { return sql.NullTime{Time: changed.Add(offset), Valid: true} }

	assert.False(t, inSync(models.RequestApproved, sql.NullTime{}, changed), "approved and never applied")
	assert.False(t, inSync(models.RequestApproved, stamp(-time.Second), changed), "a stale stamp from before the last change")
	assert.True(t, inSync(models.RequestApproved, stamp(0), changed))
	assert.True(t, inSync(models.RequestApproved, stamp(time.Second), changed))
	assert.False(t, inSync(models.RequestArchived, stamp(-time.Minute), changed), "an archive that inventory has not seen yet")

	for _, status := range []string{models.RequestDraft, models.RequestPending, models.RequestRejected} {
		assert.True(t, inSync(status, sql.NullTime{}, changed), "%s has nothing live, so there is nothing to wait for", status)
	}
}
