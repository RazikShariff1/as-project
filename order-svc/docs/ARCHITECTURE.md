# order-svc: Architecture

```
http/order      parse params/body from *gofr.Context, call the service, return httpx.Respond(...)
services/order  validation, pricing, the status lifecycle, who may see what; maps store errors to HTTP errors
stores/order    raw SQL via ctx.SQL (Postgres), one transaction per write
clients/inventory   inventory-svc over HTTP: offer (price), reserve, confirm, release
```

Contracts are in `services/interfaces.go`, `stores/interfaces.go` and `clients/inventory/inventory.go`; mocks are generated next to them
(`make generate-mocks`, mockgen v1.6.0).

## Data model

| Table | Purpose |
|---|---|
| `orders` | One row per order: `order_ref` (the reference used with inventory-svc), `customer_id` (an account-svc user id), `status`, `payment_method`, `payment_status`, `subtotal`, `discount_total`, `total`, `delivery_address` (JSONB), `notes`, `cancel_reason` |
| `order_items` | One row per (order, variant, seller) with the price frozen at order time: `mrp`, `discount`, `unit_price`, `line_total`, `tax`, `hsn_code`. `quantity` is in selling units |
| `order_logs` | Append-only history of `STATUS` and `PAYMENT` changes: `from_value`, `to_value`, `note`, `user_id` |

No cross-service foreign keys. Amounts are `NUMERIC(11,5)` rupees as in inventory-svc; totals are rounded to 2 places. Table constraints are named so a failure can be
reported (`orders_status_valid`, `orders_payment_status_valid`, `order_items_quantity_positive`, …).

## Placing an order

1. Validate the request (items, address, payment method) and merge repeated offers. A customer always orders for themselves; staff name the customer.
2. For each offer, read inventory-svc's `GET /stock/{variant}/seller/{seller}`. It must be `ENABLED` and priced.
3. Reserve stock in base units (`quantity × unit`) under a new `order_ref`, all or nothing (409 if any item is short).
4. Store the order, its items and a `PLACED` log row in one transaction. If this fails, the reservation is released.

## Status lifecycle

`PLACED → CONFIRMED → PACKED → OUT_FOR_DELIVERY → DELIVERED`; `CANCELLED` from `PLACED` or `CONFIRMED`. The store locks the row and checks the current status inside
the transaction. Stock follows the status: reserved on place, confirmed (sold) on `PACKED`, released on cancel. The status is saved first; if the stock call then fails
the API returns an error and repeating the same call retries only the stock call. A delivered COD order becomes `PAID` in the same transaction.

## Who may do what

The gateway authenticates and forwards `X-User-Id`, `X-All-Access` and `X-Seller-Scope`. A signed-in caller who is neither admin nor `sellerScope: all` is a customer: they only
see, list and cancel their own orders (someone else's is a 404), and status and payment changes answer 403. Staff roles (`order-manager`) need `sellerScope: all`, as `catalogue-manager` does.

## Not built yet

Delivery slots, disputes and returns, picking/packing queues, invoices, coupons, cashback, order editing, returns after packing, online payment (arrives with payment-svc),
change events to other services, idempotency keys on placing, and a scheduler for inventory-svc's `release-expired`.
