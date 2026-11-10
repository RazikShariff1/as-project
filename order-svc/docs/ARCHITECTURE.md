# order-svc: Architecture

The layout and the function flow follow the reference order-service: reads, placing and history are root packages, changing an order is `internal/orders`.

```
http/order              GET /order (Index), GET /order/{refNum} (Read): query -> filters.OrderDetails -> service
http/placeorder         POST /order (Create): body -> OrderPlaceCreateHTTP -> getOrderPlaceCreateSvc -> service
http/orderlog           GET /order-log (Index: Count + Find), POST /order-log (Create: GetOrderID -> Create)
internal/orders/handlers  PATCH /order/{refNum}: body -> PatchOrder -> PatchOrderPayload -> service
services/order          Find (page + count), Read: who may see what
services/placeorder     Create: validateOrderCreate -> getPlacedOrder -> processOrderItems -> reserveStock -> createOrderLogs -> store.Create
services/orderlog       Count, Find, Create, GetOrderID
internal/orders/services  Patch: load -> DeepCopy -> processStatusChange -> updateOrderItems -> address/notes/metadata -> updatePayment -> store.Update -> publishPatchEvents
stores/order            Index, Count, Retrieve, GetByClientID, Create, Update (the whole order, its items and its new logs in one transaction)
stores/orderlog         Create, GetOrderID, Count, Find
clients/inventory       inventory-svc over HTTP: offer (price), reserve, confirm, release
utils                   pricing, order references, request checks, history rows
```

Contracts are in `services/interfaces.go`, `internal/orders/services/interface.go`, `stores/interfaces.go` and `clients/inventory/inventory.go`; mocks are generated next to them
(`make generate-mocks`, mockgen v1.6.0).

## Data model

| Table | Purpose |
|---|---|
| `orders` | One row per order. `id` is the `referenceNumber` callers use; `order_ref` is the key of its stock reservation in inventory-svc (it changes when items are edited); `client_id` (unique per customer) makes placing idempotent; `customer_id` (an account-svc user id), `status`, `payment_method`, `payment_status`, `subtotal`, `discount_total`, `total`, `delivery_address` and `meta_data` (JSONB), `notes`, `cancel_reason`, `completed_at`, `stale_refs` (replaced reservations not yet released) |
| `order_items` | One row per (order, variant, seller) with the price frozen at order time: `mrp`, `discount`, `unit_price`, `line_total`, `tax`, `hsn_code`, `unit` (base units per selling unit) and `status` (`PENDING`, or `CANCELLED` for a removed line, which is kept). `quantity` is in selling units |
| `order_logs` | Append-only history as in the reference: `action` (`STATUS`, `PAYMENT`, `ITEM`, `UPDATE`, plus staff entries such as `NOTE`) and `details` (JSON) |

No cross-service foreign keys. Amounts are `NUMERIC(11,5)` rupees as in inventory-svc; totals are rounded to 2 places. Table constraints are named so a failure can be
reported (`orders_status_valid`, `orders_payment_status_valid`, `order_items_quantity_positive`, ...).

## Placing an order

1. Validate the request (items, address, payment method, metadata) and merge repeated offers. A customer always orders for themselves; staff name the customer.
2. With a `clientId`, return the customer's existing order for it, if any: no inventory calls at all.
3. Read every offer from inventory-svc (`GET /stock/{variant}/seller/{seller}`) **in parallel** (at most 8 at once). Each must be `ENABLED` and priced.
4. Reserve stock in base units (`quantity x unit`) under a new `order_ref`, all or nothing (409 if any item is short).
5. Store the order, its items (one statement) and a `PENDING` log row in one transaction. If this fails the reservation is released. If two identical
   requests race, the unique index on `(customer_id, client_id)` lets one in; the other releases its stock and returns the winner's order.

## Status lifecycle

`PENDING -> PACKED -> DISPATCHED -> COMPLETED`; `CANCELLED` from `PENDING`. No in-store processing statuses. Stock follows the status: reserved on place, confirmed (sold) on `PACKED`,
released on cancel. The status is saved first; if the stock call then fails the API returns an error and repeating the same PATCH retries only the stock call
(repeating `PACKED` or `CANCELLED` on an order already there is not an error). A completed COD order becomes `PAID` in the same transaction.

## Updating an order (PATCH)

One PATCH carries any of status, payment status, items, address, notes and metadata. As in the reference, the service loads the order (`store.Index` by reference number, scoped to the customer for a
customer), deep-copies it, applies each change to the copy (each adds its own history rows to `OrderLogs`), and saves the copy with one `store.Update`. A PATCH that changes nothing saves nothing.

Concurrent PATCHes cannot both win without a Redis lock: `Update` locks the row (`SELECT ... FOR UPDATE`) and refuses to write if `updated_at` is not the value the order was read with (409,
"changed by another request"). A pack racing a cancel: one wins, the other gets 409.

Item edits (`PENDING` only) cannot shrink a reservation in place, so they swap it: the new quantities are reserved under a new `order_ref`, the order is switched to it by the same `Update`, and the old
reference is then released. If the save fails the new reservation is released; if releasing the old one fails it stays in `stale_refs` and is retried by the next PATCH (it also expires on its own).

## Latency

An order's items come back inside its row (`json_agg`), so a read is one query; a list runs its page query and its count at the same time, and so does a history read. Place reads offers in parallel and writes
items and history in single statements; a PATCH is one load, then lock, update, history and read in one transaction.

## Who may do what

The gateway authenticates and forwards `X-User-Id`, `X-All-Access` and `X-Seller-Scope`. A signed-in caller who is neither admin nor `sellerScope: all` is a customer: they only
see, list and cancel their own orders (someone else's is a 404, and its history is empty) and may change their address and notes; status (other than cancelling), payment, items and metadata answer 403. The gateway grants customers and
`order-manager` staff the same routes (plus `POST /order-log` for staff); this service draws the line. Staff roles need `sellerScope: all`, as `catalogue-manager` does.

## Not built yet

Delivery slots, disputes and returns, picking/packing queues and in-store statuses, invoices, coupons, cashback, adding items to or substituting items on an existing order, returns after packing, online payment (arrives with payment-svc),
change events to other services (the reference publishes to Pub/Sub), Redis caching or locks (Postgres row locks do the job today), a bulk offer read in inventory-svc (offers are read one call per item, in parallel), and a scheduler for inventory-svc's `release-expired`.
