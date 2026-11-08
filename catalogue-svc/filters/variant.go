package filters

// VariantFilter narrows the variant list.
type VariantFilter struct {
	ProductID    int64
	ClientItemID string
	Barcode      string
	Status       string
	Limit        int
	Offset       int
}

// Where returns the WHERE fragment (no paging) and its args; used for both the list and count queries.
func (f *VariantFilter) Where() (query string, args []any) {
	var w where

	w.raw("deleted_at IS NULL")

	if f.ProductID != 0 {
		w.add("product_id = $%d", f.ProductID)
	}

	if f.ClientItemID != "" {
		w.add("client_item_id = $%d", f.ClientItemID)
	}

	if f.Barcode != "" {
		w.add("$%d = ANY(barcodes)", f.Barcode)
	}

	if f.Status != "" {
		w.add("status = $%d", f.Status)
	}

	return w.build()
}

// Page returns the ORDER BY/LIMIT/OFFSET fragment, appending its args after the WHERE args.
func (f *VariantFilter) Page(args []any) (query string, out []any) {
	paging, out := page(args, f.Limit, f.Offset)

	return " ORDER BY created_at, id" + paging, out
}
