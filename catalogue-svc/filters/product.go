package filters

import "fmt"

// ProductFilter narrows the product list. Query is a full-text search over name, tag line and description.
// CategoryID matches a product's primary category or any of its extra categories.
type ProductFilter struct {
	CategoryID   int64
	BrandID      int64
	Status       string
	ClientItemID string
	Barcode      string
	Query        string
	Limit        int
	Offset       int
}

// Where returns the WHERE fragment (no paging) and its args; used for both the list and count queries.
func (f *ProductFilter) Where() (query string, args []any) {
	var w where

	w.raw("deleted_at IS NULL")

	if f.CategoryID != 0 {
		w.args = append(w.args, f.CategoryID)
		n := len(w.args)
		w.clauses = append(w.clauses, fmt.Sprintf(
			"(primary_category_id = $%d OR id IN (SELECT product_id FROM category_product WHERE category_id = $%d))", n, n))
	}

	if f.BrandID != 0 {
		w.add("brand_id = $%d", f.BrandID)
	}

	if f.Status != "" {
		w.add("status = $%d", f.Status)
	}

	if f.ClientItemID != "" {
		w.add("client_item_id = $%d", f.ClientItemID)
	}

	if f.Barcode != "" {
		w.add("$%d = ANY(barcodes)", f.Barcode)
	}

	if f.Query != "" {
		w.add("search @@ plainto_tsquery('simple', $%d)", f.Query)
	}

	return w.build()
}

// Page returns the ORDER BY/LIMIT/OFFSET fragment, appending its args after the WHERE args.
func (f *ProductFilter) Page(args []any) (query string, out []any) {
	paging, out := page(args, f.Limit, f.Offset)

	return " ORDER BY created_at DESC, id" + paging, out
}
