package filters

// UserFilter narrows the user list. Name and Email match as case-insensitive prefixes; RoleID keeps users holding it.
type UserFilter struct {
	Name   string
	Email  string
	Status string
	RoleID int64
	Limit  int
	Offset int
}

// Where returns the WHERE fragment (no paging) and its args; used for both the list and count queries.
func (f *UserFilter) Where() (query string, args []any) {
	var w where

	w.raw("deleted_at IS NULL")

	if f.Name != "" {
		w.add("name ILIKE $%d", f.Name+"%")
	}

	if f.Email != "" {
		w.add("email ILIKE $%d", f.Email+"%")
	}

	if f.Status != "" {
		w.add("status = $%d", f.Status)
	}

	if f.RoleID != 0 {
		w.add("id IN (SELECT user_id FROM user_role WHERE role_id = $%d)", f.RoleID)
	}

	return w.build()
}

// Page returns the ORDER BY/LIMIT/OFFSET fragment, appending its args after the WHERE args.
func (f *UserFilter) Page(args []any) (query string, out []any) {
	paging, out := page(args, f.Limit, f.Offset)

	return " ORDER BY name, id" + paging, out
}
