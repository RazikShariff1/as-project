package filters

import "time"

// OrderDetails narrows the order list (and picks the order to read or patch). Zero values mean "any".
type OrderDetails struct {
	CustomerID       int64
	Statuses         []string
	ReferenceNumbers []int64
	PaymentStatus    string
	// PlacedFrom and PlacedTo are calendar dates (UTC); an order placed on PlacedTo itself is included.
	PlacedFrom time.Time
	PlacedTo   time.Time
	// CompletedFrom and CompletedTo are the same for the day the order was completed.
	CompletedFrom time.Time
	CompletedTo   time.Time
	// UpdatedSince keeps orders changed at or after that moment.
	UpdatedSince time.Time
	// SellerID keeps orders with at least one item from that seller.
	SellerID int64
	// ExcludeCustomerDetail leaves the shopper's details out of the answer, which saves the call to customer-svc.
	ExcludeCustomerDetail bool
	// IncludeItemNames adds each item's product name from catalogue-svc (query slice=items).
	IncludeItemNames bool
}

const day = 24 * time.Hour

// Where returns the WHERE fragment (no paging) and its args.
func (f *OrderDetails) Where() (query string, args []any) {
	var w where

	if f.CustomerID != 0 {
		w.add("customer_id = $%d", f.CustomerID)
	}

	if len(f.Statuses) > 0 {
		w.add("status = ANY($%d)", pqArray(f.Statuses))
	}

	if len(f.ReferenceNumbers) > 0 {
		w.add("id = ANY($%d)", pqArray(f.ReferenceNumbers))
	}

	if f.PaymentStatus != "" {
		w.add("payment_status = $%d", f.PaymentStatus)
	}

	if !f.PlacedFrom.IsZero() {
		w.add("created_at >= $%d", f.PlacedFrom)
	}

	if !f.PlacedTo.IsZero() {
		w.add("created_at < $%d", f.PlacedTo.Add(day))
	}

	if !f.CompletedFrom.IsZero() {
		w.add("completed_at >= $%d", f.CompletedFrom)
	}

	if !f.CompletedTo.IsZero() {
		w.add("completed_at < $%d", f.CompletedTo.Add(day))
	}

	if !f.UpdatedSince.IsZero() {
		w.add("updated_at >= $%d", f.UpdatedSince)
	}

	if f.SellerID != 0 {
		w.add("EXISTS (SELECT 1 FROM order_items oi WHERE oi.order_id = orders.id AND oi.seller_id = $%d)", f.SellerID)
	}

	return w.build()
}

// Page returns the ORDER BY/LIMIT/OFFSET fragment, appending its args after the WHERE args. Newest orders first (by id, as the
// reference service does, which is also the primary key order).
func (*OrderDetails) Page(args []any, limit, offset int) (query string, out []any) {
	paging, out := page(args, limit, offset)

	return " ORDER BY id DESC" + paging, out
}
