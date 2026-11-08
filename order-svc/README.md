# order-svc

Customers' orders: place, view, cancel, and staff moving an order through its statuses. Prices, stock and reservations come from
`inventory-svc`. Payment is cash on delivery only until `payment-svc` exists.

```bash
make create-db        # once: creates the "orders" database in the shared postgres-root container
make run              # GOFR_ENV=local, port 8084; migrations run on startup; inventory-svc must be on 8082
curl localhost:8084/.well-known/health
```

## API

Paths are singular. Lists take `limit` (default 20, max 100), `page` and `offset`. Responses are wrapped by GoFr as `{"data": ...}`.
Everything goes through the gateway as `/api/order-svc/...`.

| Call | Who | What |
|---|---|---|
| `POST /order` | customer, staff | Place an order: `items[{variantId, sellerId, quantity}]`, `deliveryAddress`, optional `paymentMethod` (`COD`), `notes`. Staff also send `customerId` |
| `GET /order` | customer (own), staff (all) | List newest first. Filters `status`, `customerId` (staff) |
| `GET /order/{id}` | owner, staff | One order with its items |
| `GET /order/{id}/log` | owner, staff | History of status and payment changes |
| `PUT /order/{id}/cancel` | owner, staff | Cancel a `PLACED` or `CONFIRMED` order and release its stock. Body `{"reason"}` |
| `PUT /order/{id}/status` | staff | `CONFIRMED`, `PACKED`, `OUT_FOR_DELIVERY`, `DELIVERED`, in that order |
| `PUT /order/{id}/payment-status` | staff, payment-svc | `PENDING`, `PAID`, `FAILED`, `REFUNDED` |

Lifecycle: `PLACED → CONFIRMED → PACKED → OUT_FOR_DELIVERY → DELIVERED`, or `CANCELLED` from `PLACED` or `CONFIRMED`. Stock is
reserved when the order is placed, confirmed as a sale when it is `PACKED`, and released on cancel. Delivering a COD order marks it `PAID`.

If inventory-svc fails after a status is saved (`PACKED` or `CANCELLED`), the call returns an error but the status stays. Repeat the same
call to retry just the stock step.

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
