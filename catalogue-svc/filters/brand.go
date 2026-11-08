package filters

// BrandFilter narrows the brand list. Name matches as a case-insensitive prefix.
type BrandFilter struct {
	Name     string
	Slug     string
	ClientID string
	Status   string
	Limit    int
	Offset   int
}

// Where returns the WHERE fragment (no paging) and its args; used for both the list and count queries.
func (f *BrandFilter) Where() (query string, args []any) {
	var w where

	w.raw("deleted_at IS NULL")

	if f.Name != "" {
		w.add("name ILIKE $%d", f.Name+"%")
	}

	if f.Slug != "" {
		w.add("slug = $%d", f.Slug)
	}

	if f.ClientID != "" {
		w.add("client_id = $%d", f.ClientID)
	}

	if f.Status != "" {
		w.add("status = $%d", f.Status)
	}

	return w.build()
}

// Page returns the ORDER BY/LIMIT/OFFSET fragment, appending its args after the WHERE args.
func (f *BrandFilter) Page(args []any) (query string, out []any) {
	paging, out := page(args, f.Limit, f.Offset)

	return " ORDER BY name, id" + paging, out
}
