# order-svc

Customers' orders: place, view, cancel, and staff moving an order through its statuses. Prices, stock and reservations come from
`inventory-svc`. Payment is cash on delivery only until `payment-svc` exists.

```bash
make create-db        # once: creates the "orders" database in the shared postgres-root container
make run              # GOFR_ENV=local, port 8084; migrations run on startup; inventory-svc must be on 8082
curl localhost:8084/.well-known/health
```

## API

Paths are singular and orders are addressed by `referenceNumber`, as in the reference order-service. Single orders answer as
`{"data": {"order": {...}}}` and lists as `{"data": {"count", "limit", "offset", "order": [...]}}`. Everything goes through the gateway
as `/api/order-svc/...`.

| Call | Who | What |
|---|---|---|
| `POST /order` | customer, staff | Place an order: `items[{variantId, sellerId, quantity}]`, `deliveryAddress`, optional `paymentMethod` (`COD`), `notes`, `metaData`, `clientId`. Staff also send `customerId`. Placing again with the same `clientId` returns the existing order |
| `GET /order` | customer (own), staff (all) | Newest first (by reference number). Filters: `id`, `referenceNumber` and `status` (comma lists), `paymentStatus`, `customerId` (staff), `sellerID` (orders with that seller's items), `placedBetween=from,to` and `completedBetween=from,to` (dates, both inclusive), `updatedSince` (RFC 3339, or a date or date-time taken as UTC). Paging: `limit` (default 20, max 100) with `offset`, or the legacy `page` with `perPage`; `paginated=false` returns up to 1000 |
| `GET /order/{refNum}` | owner, staff | One order with its items |
| `PATCH /order/{refNum}` | see below | Change an order; every field is optional and all of them apply together or not at all |
| `GET /order-log?orderReferenceNumber=` | owner, staff | History, newest first: `{limit, offset, count, orderlog:[{id, orderId, userId, action, details, createdAt}], order:{clientId}}`; optional `action` filter. An order that is not the caller's has an empty history |
| `POST /order-log` | staff | Add an entry: `{referenceNumber, action, details:{...}}`; answers `{"id"}`. STATUS, PAYMENT, ITEM and UPDATE are the system's own actions |

`PATCH /order/{refNum}` body:

| Field | Who | Effect |
|---|---|---|
| `status` | staff; a customer may only send `CANCELLED` on their own order | `PACKED`, `DISPATCHED`, `COMPLETED` in that order, or `CANCELLED` from `PENDING` |
| `cancellationReason`, `note` | as above | Kept with the order / written to its history |
| `paymentStatus` | staff, payment-svc | `PENDING`, `PAID`, `CANCELLED`, `REFUND` |
| `orderItems[{orderItemId, quantity, reason}]` | staff | While `PENDING`: set a line's quantity; `0` removes it (the last line cannot be removed: cancel instead) |
| `deliveryAddress`, `notes` | owner, staff | Until the order is dispatched |
| `metaData` | staff | Merged into the order's metadata |

Lifecycle: `PENDING → PACKED → DISPATCHED → COMPLETED`, or `CANCELLED` from `PENDING`. There are no in-store processing statuses (picking,
checking). Stock is reserved when the order is placed, confirmed as a sale when it is `PACKED`, and released on cancel. Completing a COD order marks it `PAID`.

If inventory-svc fails after a status is saved (`PACKED` or `CANCELLED`), the call returns an error but the status stays. Repeat the same
PATCH to retry just the stock step.

## Configuration

`configs/.env` holds the shared settings and `configs/.local.env` the local ones (selected by `GOFR_ENV`):

| Key | Purpose |
|---|---|
| `INVENTORY_SERVICE_URL` | Where inventory-svc is (default `http://localhost:8082`) |
| `RESERVATION_TTL_SECONDS` | How long a new order holds its stock before inventory-svc may release it (default and maximum 86400) |

Nothing releases expired holds on its own: inventory-svc's `PUT /reservation/release-expired` needs a scheduler. Until one runs, stock held by an
abandoned order stays held.

There is no service-level auth: the gateway validates every request, so this service must only be reachable from inside the platform network.

## Docs

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): layers, data model, the order flow and what is not built
- [CLAUDE.md](CLAUDE.md): guidance for coding agents
