# CLAUDE.md

Go microservice on gofr.dev + Postgres: the catalogue (brands, categories, products, variants), the seller whitelist and the
seller product request workflow for a multi-seller marketplace. Sibling repos: `inventory-svc`, `account-svc` (see the parent folder).

## Commands

```bash
make create-db         # once: "catalogue" database in the shared postgres-root container (localhost:5432)
docker start pubsub-emulator   # shared Pub/Sub emulator (project test-project, port 8085)
make run               # GOFR_ENV=local
make test              # go test ./...
make lint              # golangci-lint
make generate-mocks    # after changing services/ or stores/ interfaces
```

## Rules

- Layering is handler → service → store, through interfaces. Read `docs/ARCHITECTURE.md` first.
- **Reference implementations:** when building any catalogue or inventory API, look at
  `~/Documents/catalogue-service` and `~/Documents/inventory-service` first (routes, models, filters,
  `api/openapi.json`) and follow their conventions unless this repo's docs say otherwise.
- No service-level auth: the gateway validates requests. Do not add auth middleware here.
- Products are shared and belong to no seller. This service never stores prices or stock: they change often and live in
  inventory-svc, keyed by (variant, seller). A seller offers a product only through an approved seller product request.
- The change event is only a trigger. The database is the truth: the subscriber reads current state, so it must stay idempotent,
  and `synced_at` must only be set after a successful apply. Do not make the subscriber return an error to get a retry: GoFr's
  Google backend would not redeliver. Failures are repaired by `PUT /seller-product-request/reconcile`.
- Request state changes are guarded in the store, inside the transaction. Keep it there rather than check-then-write in the service.
- Raw SQL via `ctx.SQL`; no ORM. Never edit an applied migration; add a new key in `migrations.All()`.
- Import groups: stdlib, external, then `github.com/zopping/catalogue-svc`.
