# CLAUDE.md

Go microservice on gofr.dev + Postgres: customers' orders for a multi-seller marketplace. A slim rewrite of the core of
`~/Documents/order-service` (placing, status lifecycle, PATCH updates, history). Sibling repos: `inventory-svc` (prices, stock and
reservations), `catalogue-svc`, `account-svc`, `gateway-svc` (see the parent folder).

## Commands

```bash
make create-db         # once: "orders" database in the shared postgres-root container (localhost:5432)
make run               # GOFR_ENV=local, port 8084 (needs inventory-svc on 8082)
make test              # go test ./...
make lint              # golangci-lint
make generate-mocks    # after changing services/, stores/ or clients/inventory interfaces
```

## Rules

- Layering is handler → service → store, through interfaces. Read `docs/ARCHITECTURE.md` first. **Keep the reference's function flow**: reads, placing and logs are root packages (`http/`, `services/`, `stores/`), PATCH is `internal/orders`; a new change to an order is a new step in `Patch` that edits the deep copy and adds `OrderLogs`, not a new route or a new store method.
- **Reference implementation:** `~/Documents/order-service` and its `documentation/doc/ORDER-SERVICE.md`. Follow its naming and
  behaviour unless these docs say otherwise. Slots, disputes, picking queues, invoices, coupons and cashback are not built.
- No service-level auth: the gateway validates requests. This service only reads the identity headers (`common/caller`) to keep a
  customer to their own orders; never add auth middleware here.
- Prices and stock live in inventory-svc. An order copies the price onto its items when placed and never re-reads it.
- **API shape follows the reference:** orders are addressed by `referenceNumber` (`{refNum}`), answers are `{"order": ...}` / `{"count", "order": [...]}`, history
  is `/order-log`, and *one* `PATCH /order/{refNum}` changes an order (status, items, payment, address, notes, metadata). No separate cancel/status/payment routes, and no
  in-store processing statuses (`PENDING -> PACKED -> DISPATCHED -> COMPLETED`, `CANCELLED` from `PENDING`).
- **Order of side effects:** place = reserve stock first, then store the order (release the stock if storing fails). Pack = save the
  status, then confirm stock. Cancel = save the status, then release stock. Item edit = reserve a new reference, swap it in the transaction, release the old one. The status change is the truth: a failed stock call
  returns an error, and repeating the same call retries only the stock call. Keep it that way.
- Quantity on an order is in **selling units**; stock is reserved in **base units** (`quantity × unit`).
- Changes are guarded in the store: `Update` locks the row and refuses a stale `updated_at`. Never write around it, and never save an order read without that version. A `clientId` makes placing
  idempotent through a unique index, not through a read alone.
- Keep reads to one query and independent inventory-svc calls parallel (see "Latency" in the architecture doc).
- Every status, payment, item or detail change writes `order_logs` rows in the same transaction.
- A customer can never see another customer's order: it answers 404, not 403. Staff-only calls answer 403 to customers.
- Handlers return `httpx.Respond(...)`: GoFr turns "data plus error" into a 500, so data must be nil on error.
- Never edit an applied migration; add a new key in `migrations.All()`.
- Import groups: stdlib, external, then `github.com/zopping/order-svc`.
