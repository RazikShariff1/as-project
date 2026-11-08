package filters

// OrderFilter narrows the order list. Zero values mean "any".
type OrderFilter struct {
	CustomerID int64
	Status     string
	Limit      int
	Offset     int
}

// Where returns the WHERE fragment (no paging) and its args; used for both the list and count queries.
func (f *OrderFilter) Where() (query string, args []any) {
	var w where

	if f.CustomerID != 0 {
		w.add("customer_id = $%d", f.CustomerID)
	}

	if f.Status != "" {
		w.add("status = $%d", f.Status)
	}

	return w.build()
}

// Page returns the ORDER BY/LIMIT/OFFSET fragment, appending its args after the WHERE args. Newest orders first.
func (f *OrderFilter) Page(args []any) (query string, out []any) {
	paging, out := page(args, f.Limit, f.Offset)

	return " ORDER BY created_at DESC, id DESC" + paging, out
}
