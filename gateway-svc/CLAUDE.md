# CLAUDE.md

Go (GoFr) edge gateway for the marketplace. The only thing clients talk to: `/api/{service}/{path}` is authenticated,
authorised, scoped to the caller's seller, then proxied. Backends (`account-svc`, `catalogue-svc`, `inventory-svc`) have no auth.

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
- The `/` route exists only because GoFr does not start its HTTP server without a registered route.
