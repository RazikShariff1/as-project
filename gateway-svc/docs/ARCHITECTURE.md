# Architecture

```
client ─► gateway-svc ─► /auth/check (account-svc)
              │              └─ 401 / 403 / user + attributes + roles
              └─ policy.Enforce (ownership) ─► proxy ─► catalogue-svc | inventory-svc | account-svc
```

Packages: `routing` (parse `/api/{service}/…`, `{name}` and `/**` patterns), `policy` (route classes + ownership rules),
`accountclient` (check/me), `gateway` (middleware, proxy, identity headers), `ratelimit`, `permissions` + `seed` (roles).

## Route classes (`policy/classify.go`)
- **Public**: login (rate-limited per IP), catalogue reads, `GET /stock/availability`.
- **AuthOnly**: `GET /auth/me`, `DELETE /auth/session`: a valid token, no endpoint permission. The token is forwarded for these only.
- **Internal**: never reachable through the gateway (404): `/auth/check`, `PUT /seller-product-request/reconcile`, `PUT /reservation/release-expired`, `PUT /stock/{id}/seller/{sellerId}/offer`. Schedulers call these on the private network.
- **Protected**: everything else.

## Ownership (`policy/scope.go`)
Seller rules by path (`/stock/{id}/seller/{sellerId}`), forced query (`GET /stock`, request and whitelist lists) and JSON body
(`POST /seller-product-request`). User rules for `/user/{id}`, `/password`, `/session`. Body checks are case-insensitive on keys,
require `application/json` (415), cap size (413) and restore the body. Paths with `..`, `//`, `.`, backslash or NUL are rejected.

## Identity headers
`X-User-Id`, `X-Seller-Id` (numeric attribute `sellerId`), `X-All-Access` (admin), `X-Role-Ids`, `X-Seller-Scope: all`
(attribute `sellerScope: "all"` or admin). Backends use `X-Seller-Id`/`X-Seller-Scope` as a second line of defence
(`common/caller`). **Backends must only be reachable from the gateway**; their ports must be firewalled in any shared environment.

## Known limits
- Rate limiting is in-memory per instance.
- `/auth/check` is called on every request (no cache): simple and instantly revocable, costs one hop.
- `GET /product/{id}` is public and can return a HIDDEN product to anyone who knows the id.
- A staff user needs `sellerScope: "all"` to see across sellers; the role alone does not grant it.
- GoFr cleans `..`/`//` before the gateway runs; the cleaned path is what gets authorised and forwarded.

## The shopper realm (`/store`)

```
shopper ─► gateway-svc /store/{service}/... ─► store.Lookup (allowlist; unlisted = 404)
              ├─ Public: proxied (catalogue and stock reads, register, login; register and login are rate-limited per address)
              └─ Signed: GET /auth/me (customer-svc) ─► store.Enforce (own data) ─► proxy with X-Customer-Id
```

Separate from `/api` on purpose: different callers, tokens, id spaces and rules. Reachable services: customer-svc (the shopper's own
account), order-svc (place, list, read, cancel, history), and the public catalogue and stock reads. A shopper can only send `status:
CANCELLED`, `cancellationReason`, `note`, `deliveryAddress` and `notes` in an order PATCH; the gateway checks that (keys in any letter
case, JSON only, size capped) and order-svc enforces it again. `account-svc` is not reachable from `/store`.
