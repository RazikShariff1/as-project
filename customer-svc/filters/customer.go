package filters

// CustomerFilter narrows the customer list. Name, Email and Phone match as case-insensitive prefixes.
type CustomerFilter struct {
	Name   string
	Email  string
	Phone  string
	Status string
	Limit  int
	Offset int
}

// Where returns the WHERE fragment (no paging) and its args; used for both the list and count queries.
func (f *CustomerFilter) Where() (query string, args []any) {
	var w where

	w.raw("deleted_at IS NULL")

	if f.Name != "" {
		w.add("name ILIKE $%d", f.Name+"%")
	}

	if f.Email != "" {
		w.add("email ILIKE $%d", f.Email+"%")
	}

	if f.Phone != "" {
		w.add("phone LIKE $%d", f.Phone+"%")
	}

	if f.Status != "" {
		w.add("status = $%d", f.Status)
	}

	return w.build()
}

// Page returns the ORDER BY/LIMIT/OFFSET fragment, appending its args after the WHERE args.
func (f *CustomerFilter) Page(args []any) (query string, out []any) {
	paging, out := page(args, f.Limit, f.Offset)

	return " ORDER BY name, id" + paging, out
}
