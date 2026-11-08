package filters

// RoleFilter narrows the role list. Name matches as a case-insensitive prefix.
type RoleFilter struct {
	Name   string
	Limit  int
	Offset int
}

// Where returns the WHERE fragment (no paging) and its args; used for both the list and count queries.
func (f *RoleFilter) Where() (query string, args []any) {
	var w where

	w.raw("deleted_at IS NULL")

	if f.Name != "" {
		w.add("name ILIKE $%d", f.Name+"%")
	}

	return w.build()
}

// Page returns the ORDER BY/LIMIT/OFFSET fragment, appending its args after the WHERE args.
func (f *RoleFilter) Page(args []any) (query string, out []any) {
	paging, out := page(args, f.Limit, f.Offset)

	return " ORDER BY name, id" + paging, out
}
