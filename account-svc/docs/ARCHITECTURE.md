# account-svc: Architecture

## Layers

Every domain follows **HTTP handler → Service → Store**, each depending on the one below through an interface.

```
http/<domain>      parse params/body from *gofr.Context, call the service, return httpx.Respond(...)
services/<domain>  validation and business rules; maps store errors to HTTP errors
stores/<domain>    raw SQL via ctx.SQL (Postgres), one transaction per multi-table operation
```

Contracts are in `services/interfaces.go` and `stores/interfaces.go`; mocks are generated next to them
(`make generate-mocks`, mockgen v1.6.0).

```
main.go          wiring: stores → services → handlers → routes; request-context middleware; first-admin bootstrap
http/            auth, user, role, endpoint
services/        auth, user, role, endpoint
stores/          user, role, endpoint, session
models/          request/response structs (camelCase JSON, int64 ids)
filters/         list-endpoint filters; build WHERE/LIMIT with $n placeholders
common/          apperr (typed errors), pgerr, httpx, authctx (bearer token into handlers), password (bcrypt), pgtypes
migrations/      GoFr migrations, registered in migrations.All(); SQL embedded from init.sql
configs/         .env (shared) + .local.env, selected by GOFR_ENV
```

## Data model

IDs are `BIGINT`/`int64`, as in the other services. Column order is grouped: identity, profile, credentials/definition,
state, audit.

| Table | Purpose |
|---|---|
| `users` | `name`, `email` (lower-case, unique among live rows), `phone`, `password_hash`, `failed_logins`, `locked_until`, `last_login_at`, `status` (active/suspended), `attributes` (jsonb), soft delete |
| `roles` | `name` (unique among live rows), `description`, `all_access` |
| `user_role` | Which roles a user holds |
| `endpoints` | `service`, `method`, `path` pattern, unique per (service, method, path) among live rows |
| `endpoint_role` | Which roles may call which endpoints |
| `sessions` | `token_hash` (SHA-256, unique), `expires_at`, `revoked_at`, `user_agent`, `ip` |

Soft-deleting a user revokes their sessions; soft-deleting a role or endpoint removes its links. Assigning a role or
endpoint verifies each is live, so a deleted one cannot be assigned. There is no `organization_id`; add one if tenancy is
ever needed.

## Authentication

- **Token:** 32 random bytes, base64url (43 characters), returned once at login. Only its SHA-256 is stored, so a database
  leak cannot be replayed. Sessions last `SESSION_TTL_MINUTES` (default 720) and are opaque, not JWTs, so logout, suspension,
  role changes and password changes take effect on the next call instead of when a token expires.
- **Password:** 8 to 72 characters (bcrypt ignores the rest, so longer is rejected, not truncated), bcrypt cost 12.
- **Login:** unknown email, malformed email and wrong password all return the same 401 and spend the same time (a dummy hash
  is compared for unknown emails). Five consecutive failures lock the account for 15 minutes: counted atomically in SQL
  (`failed_logins + 1`), reported as 429, and honoured even for the correct password. A lock that has run out is cleared on the
  next attempt. A suspended account gets 403, but only after the correct password, so it cannot be probed.
- **Revocation:** `DELETE /auth/session`, `DELETE /user/{id}/session`, a password change or reset, and deleting a user all
  set `revoked_at` (password change and delete do it in the same transaction as the change).
- **Statuses:** 401 means authenticate again, 403 means authenticated but not allowed, 429 means locked. The auth error
  types implement `StatusCode()` themselves; GoFr answers 500 for any error that does not.

## Google sign-in

`POST /auth/google` takes `{ "idToken": "..." }`, the credential Google Identity Services gives the browser, and answers
exactly like `/auth/login` (201, same opaque session token). It is off until `GOOGLE_CLIENT_ID` is set (one OAuth client ID,
or several separated by commas); until then it answers 503.

- **Verification** (`common/googleid`): the token must be an RS256 JWT signed by one of Google's published keys
  (`https://www.googleapis.com/oauth2/v3/certs`, cached per Google's `Cache-Control`, refetched at most once a minute when an
  unknown key id appears), issued by `accounts.google.com`, for one of our client IDs (`aud`), unexpired, with an email. Other
  algorithms (HS256, `none`) are refused, which closes the algorithm-confusion attack. No client secret is needed or used.
- **Who may sign in:** the verified email must already belong to an active user. Nothing is ever created (this is an admin
  panel, so a Google account that merely exists must not become a user); an unknown email is 403, a suspended user 403, an
  unverified email 401.
- **Lockout:** the password lockout does not apply. It exists to stop password guessing, and a token only Google can sign is
  not a guess.
- **Failures:** a forged or foreign token is 401. If Google's keys cannot be fetched the answer is 500, not 401, because
  it is not the caller's fault.
- **Gateway:** `/auth/google` is a Public route (no bearer token), rate-limited per address together with `/auth/login`.

## Authorisation

`GET /auth/check` authenticates the token, then loads the patterns of the endpoints the user's roles grant for that
`service` and `method`, and matches the request path against them. A pattern matches when it has the same number of
segments and every literal segment is equal; `{name}` matches any one non-empty segment. A role with `all_access` skips the
match. Query strings and trailing slashes are ignored. The lookup is one query joining `user_role`, `roles`,
`endpoint_role` and `endpoints`.

## Request metadata

GoFr handlers cannot read headers, so `authctx.Middleware` copies the bearer token, the user agent and the client address
(the first `X-Forwarded-For` hop, else the socket address) into the request context. Tokens are read from the
`Authorization: Bearer` header only, never from a URL, so they stay out of access logs.

## First admin

While there are no users at all, `ADMIN_EMAIL` and `ADMIN_PASSWORD` create an `admin` role (all access) and the first user at
startup. It is a no-op once any user exists, so restarts and extra instances cannot add admins. Change the password after
first login.

## Conventions (aligned with catalogue-svc and inventory-svc)

- Singular resource routes; handlers `Index`, `Read`, `Create`, `Update`, `Delete`; pagination via `limit`, `page`, `offset`.
- Raw SQL, no ORM. Stores take `*gofr.Context` and use `ctx.SQL`.
- Errors: stores return `apperr` sentinels, services convert them with `httpx.FromStore`; a failing constraint names the field.
- GoFr answers a data-bearing `POST` with 201, so login is 201; actions that are not creations use `PUT`/`DELETE`.
- Lint: `.golangci.yml` mirrors catalogue-service (140 columns, no globals, no magic numbers).

## Not built yet

- Refresh tokens, "remember me", and listing a user's active sessions.
- Password reset by email or SMS, OTP, MFA, and social providers other than Google (the reference auth-service has OTP and
  social login). Google sign-in cannot create users or link a Google identity to an account; it matches on verified email.
- Protection against deleting or demoting the last admin.
- Per-IP rate limiting (only per-account lockout exists).
- An audit log of logins and permission changes.
- Registering a service's endpoints in bulk (today it is one `POST /endpoint` each).
- A gateway-side cache of `/auth/check` results; the service itself caches nothing.

## Auth for this service's own routes

None at service level, like the others: the gateway authorises calls to `/user`, `/role` and `/endpoint` by calling
`/auth/check` with `service=account-svc`. Register those routes as endpoints and grant them to an admin role.
