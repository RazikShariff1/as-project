# CLAUDE.md

Go microservice on gofr.dev + Postgres: generic accounts (users, roles, endpoint permissions) and login. It is
deliberately not marketplace-specific: app data goes in a user's `attributes`, never in new columns. Sibling repos:
`catalogue-svc`, `inventory-svc` (see the parent folder).

## Commands

```bash
make create-db         # once: "account" database in the shared postgres-root container (localhost:5432)
make run               # GOFR_ENV=local, port 8083
make test              # go test ./...
make lint              # golangci-lint
make generate-mocks    # after changing services/ or stores/ interfaces
```

## Rules

- Layering is handler → service → store, through interfaces. Read `docs/ARCHITECTURE.md` first.
- **Reference implementations:** `~/Documents/account-service` and `~/Documents/auth-service` are the real services;
  follow their conventions unless this repo's docs say otherwise. They carry many unrelated domains; do not copy those.
- This service is *the* place that authenticates. The other services have no auth; the gateway calls `/auth/check`.
- **Secrets:** never store, log or return a token or a password. Tokens are kept only as a SHA-256 (`HashToken`);
  passwords only as bcrypt. `User` has no hash field on purpose; keep it that way.
- **Auth errors** (`apperr.Unauthorized`, `Forbidden`, `Locked`) carry their own `StatusCode()`. Without it GoFr answers 500.
  A new auth error type needs the method and a test in `common/apperr`.
- Login must not reveal whether an email exists: unknown email and wrong password are the same 401 at the same cost
  (`burn`). A suspended account is 403 only after the correct password.
- Anything that changes a password or deletes a user must revoke sessions in the same transaction (the store does it).
- Handlers return `httpx.Respond(...)` (GoFr turns "data plus error" into a 500 otherwise).
- Never edit an applied migration; add a new key in `migrations.All()`.
- Import groups: stdlib, external, then `github.com/zopping/account-svc`.
