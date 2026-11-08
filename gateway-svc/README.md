# gateway-svc

Edge gateway: `https://host/api/{service}/{path}` → authenticate → authorise → scope to the caller's seller → proxy.

| Service | Env var | Default |
|---|---|---|
| `account-svc` | `ACCOUNT_SVC_URL` | http://localhost:8083 |
| `catalogue-svc` | `CATALOGUE_SVC_URL` | http://localhost:8081 |
| `inventory-svc` | `INVENTORY_SVC_URL` | http://localhost:8082 |

Other settings (`configs/.env`): `HTTP_PORT` 8080, `PROXY_TIMEOUT_SECONDS` 30, `MAX_BODY_BYTES` 1048576,
`LOGIN_RATE_PER_MINUTE` 10 (per IP), `TRUST_FORWARDED_FOR` false (set true only behind a trusted proxy).

## Run

```bash
# account, catalogue, inventory running first
make seed   # once, and after changing permissions/permissions.go
make run
curl -X POST localhost:8080/api/account-svc/auth/login -H 'Content-Type: application/json' \
     -d '{"email":"admin@example.com","password":"change-me-now"}'
```

Then call anything with `Authorization: Bearer <token>`.

## What it does to a request

1. Public routes (login, product/variant/brand/category reads, `GET /stock/availability`) skip auth. Product-style lists are forced to `status=ENABLED`.
2. Everything else needs a token. account-svc's `/auth/check` validates it and says whether the user's roles allow that method + path.
3. Seller-scoped routes are checked against the caller's own seller (path, query or JSON body). Wrong seller → 403;
   a seller's request by someone else's id → 404 (ids can't be probed). Staff with `sellerScope: "all"` and `admin` are exempt.
4. The request is proxied with verified identity headers; internal routes (`/auth/check`, reconcile, release-expired, offer upsert) answer 404 from outside.

See `docs/ARCHITECTURE.md`.
