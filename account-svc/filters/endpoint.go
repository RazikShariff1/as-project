package filters

// EndpointFilter narrows the endpoint list.
type EndpointFilter struct {
	Service string
	Method  string
	Limit   int
	Offset  int
}

// Where returns the WHERE fragment (no paging) and its args; used for both the list and count queries.
func (f *EndpointFilter) Where() (query string, args []any) {
	var w where

	w.raw("deleted_at IS NULL")

	if f.Service != "" {
		w.add("service = $%d", f.Service)
	}

	if f.Method != "" {
		w.add("method = $%d", f.Method)
	}

	return w.build()
}

// Page returns the ORDER BY/LIMIT/OFFSET fragment, appending its args after the WHERE args.
func (f *EndpointFilter) Page(args []any) (query string, out []any) {
	paging, out := page(args, f.Limit, f.Offset)

	return " ORDER BY service, path, method, id" + paging, out
}
