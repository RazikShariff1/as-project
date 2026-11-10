# customer-svc

Shoppers: sign up, sign in, their profile and saved addresses. Staff can look customers up and suspend them. The team's own accounts
(staff, sellers, roles) are in `account-svc`; this service never sees them.

```bash
make create-db        # once: creates the "customer" database in the shared postgres-root container
make run              # GOFR_ENV=local, port 8085; migrations run on startup
curl localhost:8085/.well-known/health
```

Shoppers reach it through the gateway as `/store/customer-svc/...`; staff as `/api/customer-svc/...`.

## API

Answers are GoFr's `{"data": ...}`. A data-bearing `POST` answers 201.

| Call | Who | What |
|---|---|---|
| `POST /auth/register` | anyone | `{name, email, phone?, password}`. Creates the customer and signs them in: `{token, expiresAt, customer}` |
| `POST /auth/login` | anyone | `{email, password}` -> `{token, expiresAt, customer}`. 401 for any bad credentials, 429 when locked, 403 when suspended |
| `GET /auth/me` | token | `{customer, expiresAt}`. The gateway calls this to authenticate every `/store` request |
| `DELETE /auth/session` | token | Sign out; repeating it is safe |
| `GET /customer/me`, `PUT /customer/me` | token | Own profile; `PUT` takes `{name, phone}` |
| `PUT /customer/me/password` | token | `{currentPassword, newPassword}`; every session ends, this one too |
| `GET/POST /customer/me/address`, `GET/PUT/DELETE /customer/me/address/{id}` | token | Saved addresses, default first. The first one saved is the default; `isDefault: true` moves the default. At most 20 |
| `GET /customer` | staff | List: `name`, `email`, `phone` (prefixes), `status`; `limit`, `page`, `offset` |
| `GET /customer/{id}` | staff | One customer |
| `PUT /customer/{id}/status` | staff | `{status: "active" | "suspended"}`; suspending ends every session |

## Configuration

`configs/.env` (shared) and `configs/.local.env` (local): `HTTP_PORT` (8085), `DB_*`, `SESSION_TTL_MINUTES` (default 10080, seven days).

## Not built yet

Email or phone verification and OTP login, password reset, social login, refresh tokens, listing active sessions, deleting an account,
and change events to other services. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).
