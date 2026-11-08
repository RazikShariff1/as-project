# inventory-svc

Owns **how many of each variant a seller can sell, and for how much** on the Zopping marketplace platform. A product is shared
catalogue content, and many sellers can sell it; here each seller has their own **offer** for a variant: their stock, price (MRP,
discount, cost, tax, HSN code), an audit log of every change, and reservations that hold their stock while an order is being paid
for, so concurrent checkouts can never oversell.

Like `inventory-service`'s `items` table (keyed by product, store and seller), it holds the details that change often. The catalogue holds
content only (names, images, barcodes) and decides which sellers may sell what. An offer is one row per **(variant, seller)**; the
variant id comes from catalogue-svc.

Built in Go on [gofr.dev](https://gofr.dev) with Postgres, following the conventions of `inventory-service` and
`catalogue-svc`.

## Quick start

```bash
make create-db       # once: creates the "inventory" database in the shared postgres-root container
make run             # GOFR_ENV=local, port 8082; migrations run on startup
curl localhost:8082/.well-known/health
```

## API

IDs are integers: `{id}` is the catalogue variant id and `{sellerId}` the seller. Lists take `limit` (default 20, max 100), `page` and
`offset`. Responses are wrapped by GoFr as `{"data": ...}`. Shapes are in [api/openapi.json](api/openapi.json).

| Route | Purpose |
|---|---|
| `GET /stock` | List offers. Filters: `variantId`, `sellerId`, `status`, `lowStock`, `outOfStock` |
| `GET /stock/{id}/seller/{sellerId}` | One seller's offer for a variant |
| `PUT /stock/{id}/seller/{sellerId}` | Set that seller's absolute quantity (creates the row). Body `{quantity, unit, status, unlimitedStock, lowStockThreshold, notes, userId}` |
| `PUT /stock/{id}/seller/{sellerId}/offer` | Create the offer or update its `mrp` and `discount`, making it visible. **Never touches quantity**, so it is safe to repeat. Called when a seller product request is approved |
| `DELETE /stock/{id}/seller/{sellerId}` | Withdraw the offer (hides it). Stock, history and existing reservations are kept |
| `PUT /stock/{id}/seller/{sellerId}/adjust` | Add or remove stock by `delta`. Optional `reason`, `notes`, `userId`, and `referenceType` + `referenceId` for idempotency |
| `PUT /stock/{id}/seller/{sellerId}/price` | Replace `mrp`, `discount`, `costPrice`, `tax`, `hsnCode` (plus `notes`, `userId`). The offer must exist |
| `GET /stock/{id}/seller/{sellerId}/movement` | The offer's quantity audit log, newest first |
| `GET /stock/{id}/seller/{sellerId}/price-log` | The offer's price-change history, newest first |
| `GET /stock/availability?variantIds=1,2,3` | Storefront view of up to 100 variants: every visible seller's offer (stock and price, never cost price), cheapest first, and the default `bestOffer` |
| `POST /reservation` | Hold stock for an order: `{orderRef, items:[{variantId, sellerId, quantity}], ttlSeconds}` |
| `GET /reservation/{orderRef}` | A reservation |
| `PUT /reservation/{orderRef}/confirm` | The order was paid: stock leaves for good |
| `PUT /reservation/{orderRef}/release` | The order was abandoned: stock becomes available again |
| `PUT /reservation/release-expired` | Release holds past their expiry; call it from a scheduler |

Confirm, release, adjust and the other actions are `PUT`s because GoFr answers a data-bearing `POST` with 201.

### Many sellers, one variant

- **Independent.** Each seller's quantity, price, movements, price log and reservations are separate. Adjusting seller A never changes seller B.
- **Offers appear through the catalogue.** When an admin approves a seller product request, catalogue-svc calls the `offer` route for each
  variant. The offer starts with zero stock; the seller then sets their quantity. Re-approving or a redelivered event only re-sets the
  price and never resets stock. Withdrawing hides the offer instead of deleting it.
- **Storefront.** `availability` lists the variant's visible offers cheapest first and flags `bestOffer`: the lowest selling price among
  sellers with stock. A price tie goes to the house seller (`HOUSE_SELLER_ID`, optional), then unlimited stock, then the larger quantity,
  then the lower seller id, so the choice never flips for the same data. Offers with no price are listed last and never win; hidden
  offers are left out.
- **Checkout.** Every reservation line names the seller it buys from. The same variant from two sellers in one order is two holds.

### How checkout uses it

1. **Reserve** every item of the order in one call. It is all or nothing: if any offer can't be covered the whole call is rejected with
   409 naming the variant and seller, and nothing is held.
2. After payment, **confirm**. If the customer abandons it (or it expires), **release**.
3. Every call is safe to repeat with the same `orderRef`: a repeated reserve returns the existing reservation, a repeated confirm or
   release changes nothing. A different quantity under the same `orderRef`, or reserving a released one, is a 409.

**Pricing.** Amounts are rupees (stored as `NUMERIC(11,5)`, shown as numbers like `249.99`). `discount` is an amount off
`mrp`, not a percentage, so `sellingPrice = mrp - discount` (computed in the database; null until an `mrp` is set). A
discount needs an `mrp` and cannot exceed it; nothing can be negative. `costPrice` is what we owe the seller per unit: it
appears on `GET /stock` and never on the storefront availability call. A seller may only write their own rows: the gateway supplies the verified
`sellerId` and checks it against the path. Price edits go through `PUT /stock/{id}/price`, not
the stock `PUT`, so editing a price can never overwrite a quantity that changed in the meantime. Every changed field is
written to the price log with its old and new value, `notes` and `userId`.

**Units.** Every quantity (stock, deltas, reservations, availability) is in base units. `unit` (default 1) says how many
base units make one selling unit, so a 20 kg item held in grams has `quantity: 20000, unit: 1000`. The service does not
convert; the caller does, using `unit`.

**Status.** `ENABLED` or `HIDDEN`. A hidden offer is left out of `availability` and rejects new reservations (409); a hold that
already exists can still be confirmed or released.

**Reasons.** `reason` on an adjustment is one of `RECEIVED`, `RETURNED_TO_SELLER`, `EXPIRED`, `DAMAGED`, `RETURNED`
(a customer return), `SOLD`, `RECOUNTED`, `OTHER` (default). Setting a quantity with `PUT` is recorded as `RECOUNTED`, and a
confirmed order as `SOLD`. `notes` and `userId` say why and who.

`available = quantity - reserved`. For `unlimitedStock` variants (e.g. made to order) reserving never fails and the
quantity is never decremented; use `inStock` rather than `available` for them.

There is no service-level auth: the gateway validates every request, so this service must only be reachable from inside
the platform network.

## Docs

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): layers, data model, the concurrency design
- [api/openapi.json](api/openapi.json): request and response shapes
- [CLAUDE.md](CLAUDE.md): guidance for coding agents
