# customer-svc: Architecture

```
http/<domain>      parse params/body, call the service, return httpx.Respond(...)
services/<domain>  validation and business rules (auth, customer, address)
stores/<domain>    raw SQL on ctx.SQL (customer, session, address); one transaction per multi-table change
```

Same conventions as account-svc: contracts in `services/interfaces.go` and `stores/interfaces.go`, mocks beside them
(`make generate-mocks`), typed errors in `common/apperr`, GoFr migrations in `migrations.All()`.

## Data model

| Table | Purpose |
|---|---|
| `customers` | `name`, `email` (lower-case, unique among live rows), `phone`, `password_hash`, `failed_logins`, `locked_until`, `last_login_at`, `status` (active/suspended), soft delete |
| `addresses` | a customer's saved addresses; one default per customer (partial unique index); soft delete |
| `sessions` | `token_hash` (SHA-256, unique), `expires_at`, `revoked_at`, `user_agent`, `ip` |

## Where a shopper's identity comes from

```
shopper ─► gateway /store/customer-svc/customer/me   (Authorization: Bearer <token>)
              ├─ GET /auth/me  ─► customer-svc   (is the token live and the customer active?)
              └─ proxy with the token forwarded ─► customer-svc resolves the caller from the token again
shopper ─► gateway /store/order-svc/order
              ├─ GET /auth/me  ─► customer-svc
              └─ proxy with X-Customer-Id: <id>, no token ─► order-svc
```

customer-svc's own routes identify the caller by the token (so they take no customer id); other services get `X-Customer-Id`. The
header is a different name from the team's `X-User-Id` because the two id spaces overlap.

## Not built yet

- Email or phone verification, OTP login, password reset, social login, MFA.
- Refresh tokens and a list of active sessions.
- Account deletion (soft delete exists in the schema; no route).
- Per-IP rate limiting here (the gateway limits register and login per address; this service only locks per account).
- Change events (a new customer, a suspension) to other services.
