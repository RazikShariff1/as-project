package filters

// PermissibleSellerFilter narrows the permissible-seller list.
type PermissibleSellerFilter struct {
	SellerID  int64
	ProductID int64
	Status    string
	Limit     int
	Offset    int
}

// Where returns the WHERE fragment (no paging) and its args; used for both the list and count queries.
func (f *PermissibleSellerFilter) Where() (query string, args []any) {
	var w where

	if f.SellerID != 0 {
		w.add("seller_id = $%d", f.SellerID)
	}

	if f.ProductID != 0 {
		w.add("product_id = $%d", f.ProductID)
	}

	if f.Status != "" {
		w.add("status = $%d", f.Status)
	}

	return w.build()
}

// Page returns the ORDER BY/LIMIT/OFFSET fragment, appending its args after the WHERE args.
func (f *PermissibleSellerFilter) Page(args []any) (query string, out []any) {
	paging, out := page(args, f.Limit, f.Offset)

	return " ORDER BY product_id, seller_id" + paging, out
}

// SellerProductRequestFilter narrows the request list.
type SellerProductRequestFilter struct {
	SellerID  int64
	ProductID int64
	Status    string
	Limit     int
	Offset    int
}

// Where returns the WHERE fragment (no paging) and its args; used for both the list and count queries.
func (f *SellerProductRequestFilter) Where() (query string, args []any) {
	var w where

	w.raw("deleted_at IS NULL")

	if f.SellerID != 0 {
		w.add("seller_id = $%d", f.SellerID)
	}

	if f.ProductID != 0 {
		w.add("product_id = $%d", f.ProductID)
	}

	if f.Status != "" {
		w.add("status = $%d", f.Status)
	}

	return w.build()
}

// Page returns the ORDER BY/LIMIT/OFFSET fragment, appending its args after the WHERE args.
func (f *SellerProductRequestFilter) Page(args []any) (query string, out []any) {
	paging, out := page(args, f.Limit, f.Offset)

	return " ORDER BY id DESC" + paging, out
}
