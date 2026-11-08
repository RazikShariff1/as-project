package filters

// StockFilter narrows the stock list. LowStock keeps limited-stock rows at or under their threshold;
// OutOfStock keeps limited-stock rows with nothing available.
type StockFilter struct {
	VariantID  int64
	SellerID   int64
	Status     string
	LowStock   bool
	OutOfStock bool
	Limit      int
	Offset     int
}

// Where returns the WHERE fragment (no paging) and its args; used for both the list and count queries.
func (f *StockFilter) Where() (query string, args []any) {
	var w where

	if f.VariantID != 0 {
		w.add("variant_id = $%d", f.VariantID)
	}

	if f.SellerID != 0 {
		w.add("seller_id = $%d", f.SellerID)
	}

	if f.Status != "" {
		w.add("status = $%d", f.Status)
	}

	if f.LowStock {
		w.raw("NOT unlimited_stock AND low_stock_threshold IS NOT NULL AND quantity - reserved <= low_stock_threshold")
	}

	if f.OutOfStock {
		w.raw("NOT unlimited_stock AND quantity - reserved <= 0")
	}

	return w.build()
}

// Page returns the ORDER BY/LIMIT/OFFSET fragment, appending its args after the WHERE args.
func (f *StockFilter) Page(args []any) (query string, out []any) {
	paging, out := page(args, f.Limit, f.Offset)

	return " ORDER BY variant_id, seller_id" + paging, out
}

// PriceLogFilter narrows a variant's price-change history, newest first.
type PriceLogFilter struct {
	VariantID int64
	SellerID  int64
	Limit     int
	Offset    int
}

// Where returns the WHERE fragment (no paging) and its args.
func (f *PriceLogFilter) Where() (query string, args []any) {
	var w where

	w.add("variant_id = $%d", f.VariantID)
	w.add("seller_id = $%d", f.SellerID)

	return w.build()
}

// Page returns the ORDER BY/LIMIT/OFFSET fragment, appending its args after the WHERE args.
func (f *PriceLogFilter) Page(args []any) (query string, out []any) {
	paging, out := page(args, f.Limit, f.Offset)

	return " ORDER BY id DESC" + paging, out
}

// MovementFilter narrows a variant's movement history, newest first.
type MovementFilter struct {
	VariantID int64
	SellerID  int64
	Limit     int
	Offset    int
}

// Where returns the WHERE fragment (no paging) and its args.
func (f *MovementFilter) Where() (query string, args []any) {
	var w where

	w.add("variant_id = $%d", f.VariantID)
	w.add("seller_id = $%d", f.SellerID)

	return w.build()
}

// Page returns the ORDER BY/LIMIT/OFFSET fragment, appending its args after the WHERE args.
func (f *MovementFilter) Page(args []any) (query string, out []any) {
	paging, out := page(args, f.Limit, f.Offset)

	return " ORDER BY id DESC" + paging, out
}
