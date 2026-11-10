package filters

// OrderLog narrows an order's history. OrderReferenceNumber and OrderID are the same number here (an order's reference number is its
// id); both are accepted as in the reference service. CustomerID restricts the history to one customer's orders.
type OrderLog struct {
	OrderReferenceNumber string
	OrderID              string
	Action               string
	CustomerID           int64
}

// Where returns the WHERE fragment and its args, over order_logs joined to orders.
func (f *OrderLog) Where() (query string, args []any) {
	var w where

	if f.OrderReferenceNumber != "" {
		w.add("orders.id = $%d::bigint", f.OrderReferenceNumber)
	}

	if f.OrderID != "" {
		w.add("order_logs.order_id = $%d::bigint", f.OrderID)
	}

	if f.Action != "" {
		w.add("order_logs.action = $%d", f.Action)
	}

	if f.CustomerID != 0 {
		w.add("orders.customer_id = $%d", f.CustomerID)
	}

	return w.build()
}
