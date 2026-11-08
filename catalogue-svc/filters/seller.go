package filters

// SellerFilter narrows the seller list.
type SellerFilter struct {
	Status string
	Limit  int
	Offset int
}

// Clause returns the WHERE/LIMIT/OFFSET fragment and its args.
func (f *SellerFilter) Clause() (query string, args []any) {
	var w where

	if f.Status != "" {
		w.add("status = $%d", f.Status)
	}

	query, args = w.build()
	paging, args := page(args, f.Limit, f.Offset)

	return query + " ORDER BY created_at, id" + paging, args
}
