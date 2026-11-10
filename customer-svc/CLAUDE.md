# CLAUDE.md

Go microservice on gofr.dev + Postgres: **shoppers' accounts** (profile, saved addresses, login). Sibling of `account-svc`, which holds
the *team* (staff and sellers). Shoppers and team members are different people with different tokens and different id spaces; never
mix them. Sibling repos: `account-svc`, `catalogue-svc`, `inventory-svc`, `order-svc`, `gateway-svc` (see the parent folder).

## Commands

```bash
make create-db         # once: creates the "customer" database in the shared postgres-root container
make run               # GOFR_ENV=local, port 8085; migrations run on startup
make test              # go test ./...
make generate-mocks    # after changing services/ or stores/interfaces.go
```

## Rules

- Layers: `http/<domain>` -> `services/<domain>` -> `stores/<domain>`, wired in `main.go`. Handlers return `httpx.Respond(...)`.
- **No service-level auth, except the "me" routes.** The gateway authorises staff routes (`GET /customer`, `/customer/{id}`,
  `PUT /customer/{id}/status`) by role. Never put those on the gateway's `/store` realm.
- **A shopper only ever reaches their own data because their routes take no id.** `/auth/me`, `/customer/me...` resolve the caller from
  the bearer token (`authctx.Token`), which the gateway forwards for exactly these routes. Never add a route under `/customer/me` that
  reads a customer id from the path, query or body. Every address store call takes the customer id as well, so another customer's
  address is "not found".
- Tokens are opaque, 32 random bytes; only their SHA-256 is stored. Login answers the same 401 and costs the same time for an
  unknown email and a wrong password; five consecutive failures lock the account for 15 minutes (429). A suspended customer gets 403
  only after the right password, and is refused on every authenticated call at once. Suspending, and changing the password, revoke
  every session in the same transaction as the change.
- Passwords are 8 to 72 characters (bcrypt ignores the rest, so longer is rejected, not truncated).
- At most one default address per customer: a partial unique index, plus a row lock on the customer inside the address transactions.
- Never edit an applied migration; add a new key in `migrations.All()`.
