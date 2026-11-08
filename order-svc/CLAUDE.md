# CLAUDE.md

Go microservice on gofr.dev + Postgres: customers' orders for a multi-seller marketplace. A slim rewrite of the core of
`~/Documents/order-service` (placing, status lifecycle, cancel, history). Sibling repos: `inventory-svc` (prices, stock and
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

- Layering is handler → service → store, through interfaces. Read `docs/ARCHITECTURE.md` first.
- **Reference implementation:** `~/Documents/order-service` and its `documentation/doc/ORDER-SERVICE.md`. Follow its naming and
  behaviour unless these docs say otherwise. Slots, disputes, picking queues, invoices, coupons and cashback are not built.
- No service-level auth: the gateway validates requests. This service only reads the identity headers (`common/caller`) to keep a
  customer to their own orders; never add auth middleware here.
- Prices and stock live in inventory-svc. An order copies the price onto its items when placed and never re-reads it.
- **Order of side effects:** place = reserve stock first, then store the order (release the stock if storing fails). Pack = save the
  status, then confirm stock. Cancel = save the status, then release stock. The status change is the truth: a failed stock call
  returns an error, and repeating the same call retries only the stock call. Keep it that way.
- Quantity on an order is in **selling units**; stock is reserved in **base units** (`quantity × unit`).
- Status changes are guarded in the store, inside the transaction (`SELECT … FOR UPDATE`). Do not check-then-write in the service.
- Every status or payment change writes an `order_logs` row in the same transaction.
- A customer can never see another customer's order: it answers 404, not 403. Staff-only calls answer 403 to customers.
- Handlers return `httpx.Respond(...)`: GoFr turns "data plus error" into a 500, so data must be nil on error.
- Never edit an applied migration; add a new key in `migrations.All()`.
- Import groups: stdlib, external, then `github.com/zopping/order-svc`.
