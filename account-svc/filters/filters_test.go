package filters

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUserFilter(t *testing.T) {
	where, args := (&UserFilter{}).Where()
	assert.Equal(t, " WHERE deleted_at IS NULL", where, "soft-deleted users are always excluded")
	assert.Empty(t, args)

	f := &UserFilter{Name: "as", Email: "asha@", Status: "active", RoleID: 4, Limit: 20, Offset: 40}

	where, args = f.Where()
	assert.Equal(t, " WHERE deleted_at IS NULL AND name ILIKE $1 AND email ILIKE $2 AND status = $3"+
		" AND id IN (SELECT user_id FROM user_role WHERE role_id = $4)", where)
	assert.Equal(t, []any{"as%", "asha@%", "active", int64(4)}, args)

	paging, args := f.Page(args)
	assert.Equal(t, " ORDER BY name, id LIMIT $5 OFFSET $6", paging)
	assert.Equal(t, []any{"as%", "asha@%", "active", int64(4), 20, 40}, args)
}

func TestRoleFilter(t *testing.T) {
	f := &RoleFilter{Name: "sel", Limit: 10}

	where, args := f.Where()
	assert.Equal(t, " WHERE deleted_at IS NULL AND name ILIKE $1", where)

	paging, args := f.Page(args)
	assert.Equal(t, " ORDER BY name, id LIMIT $2 OFFSET $3", paging)
	assert.Equal(t, []any{"sel%", 10, 0}, args)
}

func TestEndpointFilter(t *testing.T) {
	f := &EndpointFilter{Service: "catalogue-svc", Method: "GET", Limit: 5}

	where, args := f.Where()
	assert.Equal(t, " WHERE deleted_at IS NULL AND service = $1 AND method = $2", where)

	paging, _ := f.Page(args)
	assert.Equal(t, " ORDER BY service, path, method, id LIMIT $3 OFFSET $4", paging)
}
