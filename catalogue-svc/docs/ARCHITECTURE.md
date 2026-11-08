# catalogue-svc: Architecture

## Layers

Every domain follows **HTTP handler → Service → Store**, each depending on the one below through an interface.

```
http/<domain>      parse params/body from *gofr.Context, call the service, return (any, error)
services/<domain>  validation and business rules; maps store errors to HTTP errors
stores/<domain>    raw SQL via ctx.SQL (Postgres); maps driver errors to apperr sentinels
```

Contracts are in `services/interfaces.go` and `stores/interfaces.go`. Mocks are generated next to them
(`make generate-mocks`, mockgen v1.6.0).

## Layout

```
main.go          wiring: stores → services → handlers → routes; inventory-svc client; event subscription
http/            one package per resource
services/        one package per domain
stores/          one package per store
models/          request/response structs (camelCase JSON, int64 ids)
filters/         list-endpoint filters; build WHERE/LIMIT with $n placeholders
common/          apperr (sentinels and typed errors), pgerr, httpx (GoFr errors, pagination), events (publisher)
clients/         inventory (calls inventory-svc to make a seller offer live or hide it)
eventhandler/    sellerproductchange (the pub/sub subscriber)
migrations/      GoFr migrations, registered in migrations.All(); SQL embedded from init.sql
configs/         .env (shared) + .local.env, selected by GOFR_ENV
```

## Conventions (aligned with catalogue-service)

- Singular resource routes (`/seller`, `/category`); handler methods `Index`, `Read`, `Create`, `Update`.
- `PUT` replaces; pagination via `limit` (default 20, max 100), `page`, `offset`.
- Raw SQL, no ORM. Stores take `*gofr.Context` and use `ctx.SQL`.
- Errors: stores return `apperr` sentinels, services convert them with `httpx.FromStore` (a foreign-key failure names the
  offending field, e.g. `brandId`).
- Handlers return `httpx.Respond(...)`: GoFr turns "data plus error" into a 500, so data must be nil on error.
- Lint: `.golangci.yml` mirrors catalogue-service (140 columns, no globals, no magic numbers).

## Data model

Tables and column names follow catalogue-service (`db/initial_schema.sql` and its migrations), translated to Postgres.

- IDs are `BIGINT GENERATED ALWAYS AS IDENTITY` and `int64` in Go, matching catalogue-service and inventory-service so ids
  travel between services unchanged. Malformed ids are a 400.
- `sellers`, `brands`, `categories`, `products`, `category_product` (extra categories), `variants`.
- **Products belong to no seller.** They are shared content; the same product is offered by many sellers.
- Status is `ENABLED`/`HIDDEN`; `deleted_at` is the soft delete. Reads and unique indexes ignore deleted rows.
- Products and variants carry `images`/`videos` (text[]), `barcodes` (text[]), `meta_data` (jsonb) and an external `client_item_id`.
- No prices, tax or stock: they change often and vary by seller, so inventory-svc holds them per (variant, seller), as
  inventory-service's `items` table does. A variant is content only.
- `permissible_product_sellers (seller_id, product_id, status ENABLED/DISABLED)`: the admin-managed whitelist.
- `seller_product_requests` (status DRAFT, PENDING, REJECTED, APPROVED, ARCHIVED; `rejection_reason`; `product_status` ENABLED/HIDDEN;
  `synced_at`) and `seller_product_items (request_id, variant_id, mrp, discount)`. A partial unique index allows at most one
  APPROVED request per (seller, product).
- Deliberately not copied yet from catalogue-service: `organization_id`, tags, custom properties, collections, ratings,
  substitution groups, seller-proposed content changes (description, images), and approval e-mails.

## Seller offers: workflow, event and reconciliation

The state machine is enforced in the store, inside the transaction that changes it, so two admins acting at once cannot both win:

```
DRAFT --submit--> PENDING --approve--> APPROVED --(a newer approval)--> ARCHIVED
  ^                  |                    |
  |                  +--reject--> REJECTED --(revise + resubmit)--> PENDING   (REJECTED can also be approved by an admin)
  +-- seller edits (DRAFT or REJECTED only)
```

- **Approve** archives the seller's previous approved request for the product and approves the new one in one transaction; approving an
  already-approved request changes nothing and re-sends the event.
- **Event.** `seller-product-change`, payload `{"sellerProductRequestIDs": [...]}`. Like catalogue-service's event it carries only ids.
  This service subscribes to it itself.
- **Apply.** For each id the subscriber reads the request's *current* state and makes inventory match: APPROVED and ENABLED upserts each
  item's offer (`PUT /stock/{variant}/seller/{seller}/offer`, which never touches quantity); APPROVED and HIDDEN withdraws it; ARCHIVED
  withdraws only the variants the seller no longer has live; everything else is ignored. Because it reads current state, applying an
  event twice, late, or out of order ends in the same place, and a redelivered approval can never wipe stock.
- **Sync tracking.** After a successful apply the request gets `synced_at`, but only if its `updated_at` is unchanged since it was read,
  so a change made in the meantime stays unsynced. `synced` is true when the stamp is not older than the last change (always true for
  requests with nothing live).
- **Why reconcile exists.** The reference returns an error from the subscriber to get a retry. With GoFr's Google Pub/Sub backend that
  does not work: GoFr never rejects the message, so the Google client library keeps extending its acknowledgement deadline (an hour by
  default) and the broker does not redeliver, while the message occupies a slot. So the subscriber logs a failed apply and acknowledges
  the message (a malformed one is dropped), and `PUT /seller-product-request/reconcile` applies everything with `synced = false`. A
  scheduler calls it, in the same way as inventory-svc's `release-expired`. This also repairs an approval whose event could not be
  published (the call returns an error saying it was saved; the next reconcile applies it).
- **inventory-svc down.** Approvals still succeed and are saved; offers go live on the first reconcile after it returns. While it is down,
  GoFr reports this service's health as `DEGRADED` because it checks the services it calls.

## Conventions (aligned with catalogue-service)

- Singular resource routes; handlers `Index`, `Read`, `Create`, `Update`, `Delete`; pagination via `limit`, `page`, `offset`.
- `PUT` replaces. Raw SQL, no ORM. Stores take `*gofr.Context` and use `ctx.SQL`.
- Errors: stores return `apperr` sentinels, services convert them with `httpx.FromStore` (a foreign-key failure names the field).
  `Forbidden` (403) and `StateConflict` (409) carry their own status code, because GoFr answers 500 for an error without one.
- Handlers return `httpx.Respond(...)`: GoFr turns "data plus error" into a 500, so data must be nil on error.
- Lint: `.golangci.yml` mirrors catalogue-service (140 columns, no globals, no magic numbers).

## Auth

None at service level. The gateway authenticates and authorises every request.
