# inventory-svc: Architecture

## Layers

Every domain follows **HTTP handler → Service → Store**, each depending on the one below through an interface.

```
http/<domain>      parse params/body from *gofr.Context, call the service, return httpx.Respond(...)
services/<domain>  validation and business rules; maps store errors to HTTP errors
stores/<domain>    raw SQL via ctx.SQL (Postgres), one transaction per operation
```

Contracts are in `services/interfaces.go` and `stores/interfaces.go`; mocks are generated next to them
(`make generate-mocks`, mockgen v1.6.0).

```
main.go          wiring: stores → services → handlers → routes
http/            stock, reservation
services/        stock, reservation
stores/          stock, reservation
models/          request/response structs (camelCase JSON, int64 ids)
filters/         list-endpoint filters; build WHERE/LIMIT with $n placeholders
common/          apperr (sentinels and typed errors), pgerr (Postgres error mapping), httpx (GoFr errors, parsing)
migrations/      GoFr migrations, registered in migrations.All(); SQL embedded from init.sql
configs/         .env (shared) + .local.env, selected by GOFR_ENV
```

## Data model

IDs are `BIGINT`/`int64`, matching the other services. A row is one seller's offer for a variant: the key is **(variant_id, seller_id)**,
as inventory-service keys items by product and seller. There is no cross-service foreign key: `variant_id` is a catalogue-svc
`variants.id` and `seller_id` a seller id, and nothing here checks that either exists. Offers are created when catalogue-svc approves a
seller product request, so in practice only whitelisted sellers and real variants get rows.

| Table | Purpose |
|---|---|
| `stock` | One row per (variant, seller). Columns in groups: `variant_id`, `seller_id`; stock levels `quantity` (on hand), `reserved`, `unlimited_stock`, `low_stock_threshold`, `unit`; `status`; commercial details `mrp`, `discount`, `cost_price`, `tax`, `hsn_code`; `created_at`, `updated_at`. Available is `quantity - reserved`; selling price is `mrp - discount` |
| `stock_movements` | Append-only audit log: `delta`, `quantity_after`, `reason`, `notes`, `user_id`, and a business reference |
| `stock_price_log` | Append-only audit of price changes: `field` (MRP, DISCOUNT, COST_PRICE, TAX, HSN_CODE), `old_value`, `new_value`, `notes`, `user_id` |
| `stock_reservations` | One row per (order, variant, seller): `quantity`, `status` RESERVED/CONFIRMED/RELEASED, `expires_at` |

Table constraints are the last line of defence and are named so they can be reported: `stock_quantity_nonneg`,
`stock_reserved_nonneg`, `stock_reserved_within_quantity` (reserved can never exceed quantity unless unlimited),
`stock_unit_positive`, `stock_status_valid`, `stock_movements_reason_valid`, `stock_mrp_nonneg`, `stock_discount_nonneg`,
`stock_cost_price_nonneg` and `stock_discount_within_mrp`.

**Why price lives here.** inventory-service keeps `mrp`, `discount` and `tax` on its `items` row and audits price changes next to
stock, and its search indexer computes the selling price as `mrp - discount`. Those values change far more often than a
product's content and vary by seller, so they sit with stock and the catalogue stays content-only. `cost_price` is our
marketplace addition (what we owe the seller). Amounts are `NUMERIC(11,5)` rupees as in the reference, so other services read
prices the same way; `sellingPrice` is computed in SQL to stay exact. Stock and price are written by separate endpoints and
transactions on purpose: a full-row `PUT` for a price edit could overwrite a quantity that changed meanwhile.

**Units.** All quantities are base units (as in inventory-service, where `items.stock` holds the product's base unit,
e.g. grams). `unit` is metadata: base units per selling unit. inventory-service converts server-side (`delta x unit`);
this service does not, so callers send and read base units. Conversion on the server is a possible later change.

**Movement reasons** follow inventory-service's list without its distribution-centre ones (`RECEIVED_FROM_DC`,
`RETURNED_TO_DC`), because a marketplace has sellers, not DCs: `RECEIVED`, `RETURNED_TO_SELLER`, `EXPIRED`, `DAMAGED`,
`RETURNED`, `SOLD`, `RECOUNTED`, `OTHER`. A `PUT` of an absolute quantity is `RECOUNTED`; a confirmed order is `SOLD`.

**Status.** `HIDDEN` stock reports `available = 0` and `inStock = false` in `/stock/availability`, and reservation
rejects it. Existing holds are unaffected.

## How overselling is prevented

Reserving is a single guarded UPDATE per item, inside one transaction for the whole order:

```sql
UPDATE stock SET reserved = reserved + $qty
 WHERE variant_id = $id AND seller_id = $seller AND status = 'ENABLED' AND (unlimited_stock OR quantity - reserved >= $qty)
```

Postgres locks the row for the duration of the statement, so two concurrent orders cannot both pass the check; the loser
matches zero rows and the whole order is rolled back with a 409 (`InsufficientStock`). Verified with 30 concurrent
orders against 10 units: exactly 10 succeed.

- **Deadlocks:** the service sorts items by (variant, seller) before calling the store, and confirm/release read rows
  `ORDER BY variant_id, seller_id FOR UPDATE`, so every order takes row locks in the same order.
- **All or nothing:** one transaction per order; any failing item rolls everything back.
- **Idempotency:** `stock_reservations` is unique on `(order_ref, variant_id, seller_id)`. A repeat with the same items returns the
  existing reservation without holding more; a different quantity or a released reservation is a 409. Confirm and release
  skip items already in the target state. Adjustments carry an optional `(referenceType, referenceId)`; a unique index on
  `stock_movements` rejects a repeat, rolling the repeated change back.
- **Confirm** subtracts from `quantity` and `reserved` and writes a `SALE` movement (delta = -qty, reference `ORDER`).
  **Release** only subtracts from `reserved`. Unlimited-stock variants are never decremented and write no movement.
- **Expiry:** each hold has `expires_at` (default 15 minutes, max 24 hours, from the database clock). Nothing expires on its
  own: a scheduler must call `PUT /reservation/release-expired`. Until it runs, expired stock stays held, and an order
  confirmed before then still succeeds.

## Many sellers, one product

The catalogue's products are shared, so the same variant has an offer per seller. The design follows inventory-service, whose `items`
table is keyed by product and seller.

- **Independence.** Quantity, reservations, price, the movement log and the price log are all per (variant, seller). A reference
  (`ORDER`, `GRN`, ...) is unique per offer, so the same order id may appear on two sellers' rows but never twice on one.
- **Offer upsert** (`PUT /stock/{id}/seller/{sellerId}/offer`): one `INSERT ... ON CONFLICT (variant_id, seller_id) DO UPDATE` that sets
  only `mrp`, `discount` and `status = ENABLED`, with the price changes logged in the same transaction. It never writes `quantity`. This
  matters because catalogue-svc calls it from a pub/sub subscriber and from a reconcile job: the same approval can arrive more than
  once, and a repeat must not reset a seller's stock to zero.
- **Withdraw** (`DELETE`) only sets `status = HIDDEN`. Rows are never deleted, so history and reservations stay intact, and a later
  approval brings the same row back with its stock.
- **Storefront ranking** (`availability`): the store returns each variant's visible offers (`ENABLED` only); the service sorts them by
  selling price (offers without a price last) and picks `bestOffer` as the first offer that is in stock and priced. Ties break on the
  house seller (`HOUSE_SELLER_ID`, 0 for none), then unlimited stock, then the larger quantity, then the lower seller id.
- **Who may write what.** There is no service-level auth, so nothing here stops seller A calling seller B's routes. The gateway must take
  the verified `sellerId` from the caller's account (`attributes.sellerId`) and refuse a request whose `{sellerId}` differs, except for
  admin and for catalogue-svc's own offer calls.

## Conventions (aligned with catalogue-svc and inventory-service)

- Singular resource routes; handler methods `Index`, `Read`, `Create`, `Put`, ...; pagination via `limit`, `page`, `offset`.
- Raw SQL, no ORM. Stores take `*gofr.Context` and use `ctx.SQL`.
- Errors: stores return `apperr` sentinels or typed errors, services convert them with `httpx.FromStore`.
  `InsufficientStock` and `StateConflict` become 409 with their own message; a failing constraint names the field.
- Lint: `.golangci.yml` mirrors catalogue-service (140 columns, no globals, no magic numbers).

## Not built yet

- `store_id` / multiple locations per offer (inventory-service keys items by store, product and seller; this keys by variant and seller).
- Pricing tiers (`pricing_tiers` in inventory-service) and multiple MRPs per item (`item_multiple_mrp`).
- Bulk CSV upload of stock (`inventory_files` in inventory-service).
- Server-side unit conversion (inventory-service multiplies deltas by `unit`).
- Lots, batches, expiry dates and bins (`stock_positions` in inventory-service).
- Low-stock alerts and change events to other services (inventory-service publishes stock deltas on a topic).
- A scheduler that calls `release-expired`.

## Auth

None at service level. The gateway authenticates and authorises every request.
