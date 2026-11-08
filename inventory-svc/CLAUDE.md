# CLAUDE.md

Go microservice on gofr.dev + Postgres: per-seller offers (stock and price), their audit logs and order reservations for a
multi-seller marketplace where many sellers sell the same shared product. Sibling repos: `catalogue-svc` (owns products, variants, prices), `account-svc` (see the parent folder).

## Commands

```bash
make create-db         # once: "inventory" database in the shared postgres-root container (localhost:5432)
make run               # GOFR_ENV=local, port 8082
make test              # go test ./...
make lint              # golangci-lint
make generate-mocks    # after changing services/ or stores/ interfaces
```

## Rules

- Layering is handler → service → store, through interfaces. Read `docs/ARCHITECTURE.md` first.
- **Reference implementations:** when building any inventory or catalogue API, look at
  `~/Documents/inventory-service` and `~/Documents/catalogue-service` first and follow their conventions unless this
  repo's docs say otherwise.
- No service-level auth: the gateway validates requests. Do not add auth middleware here.
- An offer is one row per **(variant, seller)**. This service owns stock **and** price, tax and HSN code (the details that change
  often); it never stores product content. The variant id is the catalogue's; the seller id is the seller's.
- Sellers' data is independent: every query, log row, constraint and reservation line carries the seller. Never look up by variant alone.
- `UpsertOffer` (called when a seller product request is approved) must never write quantity: events are redelivered.
- `costPrice` must never appear in the storefront `availability` response.
- Price edits go through `SetPrice` (own transaction + price log); `Put` must never write price columns.
- **Every quantity change writes a `stock_movements` row in the same transaction.** Never `UPDATE stock` on its own.
- Reservation SQL locks stock rows in ascending (variant, seller); keep it that way or concurrent orders can deadlock.
- Handlers return `httpx.Respond(...)`: GoFr turns "data plus error" into a 500, so data must be nil on error.
- Never edit an applied migration; add a new key in `migrations.All()`.
- Import groups: stdlib, external, then `github.com/zopping/inventory-svc`.
