# CLAUDE.md

Go (GoFr) edge gateway for the marketplace. The only thing clients talk to. Two realms:

- `/api/{service}/{path}` is the **team's**: authenticated and authorised by `account-svc`, scoped to the caller's seller, then proxied.
- `/store/{service}/{path}` is the **shoppers'**: a fixed allowlist (package `store`), authenticated by `customer-svc`, held to the
  shopper's own data, then proxied with `X-Customer-Id`.

Backends have no auth of their own.

## Commands

```bash
make run        # port 8080, backends from configs/.env
make test       # go test ./...
make seed       # register endpoints + roles in account-svc (re-runnable)
```

## Rules

- **Fail closed.** Anything that is not explicitly Public/AuthOnly is Protected; if account-svc is unreachable or answers
  oddly, the request is refused (never allowed through).
- Route classes live in `policy/classify.go`. A new public or internal route must be added there deliberately, with a test.
- Ownership rules live in `policy/scope.go`. A new seller-keyed route in a backend needs a rule here AND the backend's own
  `common/caller` backstop. Keep the bypass tests (case-insensitive JSON keys, form bodies, oversize) passing.
- The token is read before `stripIdentity`. Backends never see `Authorization`/`Cookie`; the one exception is the AuthOnly routes
  (`/auth/me`, `/auth/session`), because account-svc identifies the caller by the token itself.
- Identity reaches backends only as `X-User-Id`, `X-Seller-Id`, `X-All-Access`, `X-Role-Ids`, `X-Seller-Scope`. Client-supplied
  versions of these (and any `X-User-*`, `X-Seller-*`, `X-All-*`, `X-Role-*`) are stripped first.
- Backend `Access-Control-*` headers are dropped; the gateway sets CORS once.
- `permissions/permissions.go` is the source of truth for roles; a guard test fails if a role grants something the seller must not have.
- **`/store` is an allowlist, not a role check.** A call not listed in `store/store.go` is a 404. A signed route there needs an
  ownership rule (`OwnQuery`, `OwnBody`, `OnlyBody`/`BodyValues`) or, for customer-svc, must be a token-identified `/me` route. Never
  list a staff route in `store`; `store/store_test.go` fails if one slips in. Shoppers get `X-Customer-Id`, never `X-User-Id`, and
  `X-Customer-*` is stripped from clients like the other identity headers. A team token does not work on `/store` and a shopper
  token does not work on `/api`.
- **Every `/api` and `/store` request logs one line** when it is done (`gateway/accesslog.go`): `realm`, `method`, `path`, `service`,
  `decision` (public, protected, allowlisted-signed, not-listed, own-data-denied, ...), `who` (`user:<id>`, `customer:<id>` or
  `anonymous`), `target` (where it was proxied; empty when it never left), `status`, `ms`, `cid`. Refusals are warnings. Never log
  tokens, query strings or bodies; `accesslog_test.go` checks that.
- The `/` route exists only because GoFr does not start its HTTP server without a registered route.
