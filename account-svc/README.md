# account-svc

A generic accounts service: **users, roles, permissions and login**. It knows nothing about the marketplace; what a user
may do is data (roles mapped to the endpoints of other services), and anything app-specific rides along as free-form user
`attributes` (for example `{"sellerId": 12}`).

It is the other half of the gateway's job. The gateway authenticates and authorises every request by calling
`GET /auth/check`; the other services trust the gateway and have no auth of their own.

Built in Go on [gofr.dev](https://gofr.dev) with Postgres, in the same layout as `catalogue-svc` and `inventory-svc`.

## Quick start

```bash
make create-db       # once: creates the "account" database in the shared postgres-root container
make run             # GOFR_ENV=local, port 8083; migrations run on startup
curl localhost:8083/.well-known/health
```

On first start, while there are no users, an admin is created from `ADMIN_EMAIL` and `ADMIN_PASSWORD` (set in
`configs/.local.env` for development; set real values elsewhere, then change the password). The admin holds an `admin`
role with `allAccess`, which may call every endpoint.

## The gateway contract

For each incoming request the gateway calls:

```
GET /auth/check?service=catalogue-svc&method=PUT&path=/product/42
Authorization: Bearer <token from the client>
```

| Response | Meaning |
|---|---|
| `200` | Allowed. The body is the principal: `user` (with `roleIds` and `attributes`), `allAccess`, `expiresAt` |
| `401` | No token, or it is unknown, expired or revoked: the client must log in again |
| `403` | Known user but not allowed: suspended account, or no role grants this method and path |

The gateway can forward `user.id` and `user.attributes` to the service (for example, to limit a seller to their own
`sellerId`). Role and status changes apply on the very next call. Cache a `200` for a few seconds at most.

## API

IDs are integers. Lists take `limit` (default 20, max 100), `page` and `offset`. Responses are wrapped by GoFr as
`{"data": ...}`. Shapes are in [api/openapi.json](api/openapi.json).

| Route | Purpose |
|---|---|
| `POST /auth/login` | `{email, password}` returns `{token, expiresAt, user}` (201). The token is shown once |
| `GET /auth/me` | Who the bearer token belongs to |
| `GET /auth/check` | The gateway call above |
| `DELETE /auth/session` | Log out (revoke the bearer token). Safe to repeat |
| `GET/POST /user`, `GET/PUT/DELETE /user/{id}` | Users. Filters: `name`, `email` (prefixes), `status`, `roleId` |
| `PUT /user/{id}/password` | Change a password: `{currentPassword, newPassword}` |
| `PUT /user/{id}/password/reset` | Set a password without the old one: `{newPassword}`. Give it its own permission |
| `DELETE /user/{id}/session` | Sign a user out everywhere |
| `GET/POST /role`, `GET/PUT/DELETE /role/{id}` | Roles. `allAccess` and the `endpointIds` they may call |
| `GET/POST /endpoint`, `GET/PUT/DELETE /endpoint/{id}` | Endpoints: `service`, `method`, `path`. Filters: `service`, `method` |

- **Permissions.** An endpoint is a service, a method and a path pattern. In a pattern, `{name}` matches exactly one path
  segment, so `/product/{id}` matches `/product/42` but not `/product` or `/product/42/variant`. Query strings and trailing
  slashes are ignored. A role with `allAccess` may call anything, registered or not.
- **Users.** Email is unique, stored lower-case. Status is `active` or `suspended`. `PUT /user/{id}` replaces the profile,
  status, attributes and `roleIds`, and never touches the password. `DELETE` is a soft delete that also signs the user out.
- **Passwords** are 8 to 72 characters and stored as bcrypt hashes. Changing or resetting one signs the user out everywhere.
- **Login** gives the same `401` for an unknown email and a wrong password. Five wrong passwords lock the account for 15
  minutes (`429`), even for the right password; a reset clears the lock. A suspended account gets `403` only after the correct
  password.

## Docs

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): layers, data model, auth and security design
- [api/openapi.json](api/openapi.json): request and response shapes
- [CLAUDE.md](CLAUDE.md): guidance for coding agents
