# CLAUDE.md

Go microservice on gofr.dev + Postgres: **sends emails**. Modelled on the reference `~/Documents/communication-service` (same
`POST /email` body: `to`, `cc`, `bcc`, `type`, `data`, `language`; same template syntax), cut down to email only, one tenant, SMTP
credentials from env. Sibling repos: `account-svc`, `catalogue-svc`, `inventory-svc`, `order-svc`, `customer-svc`, `gateway-svc`.

## Commands

```bash
make create-db         # once: creates the "communication" database in the shared postgres-root container
make run               # GOFR_ENV=local, port 8086; migrations run on startup
make test              # go test ./...
make generate-mocks    # after changing services/ or stores/interfaces.go
```

## Rules

- Layers: `http/<domain>` -> `services/<domain>` -> `stores/<domain>`, wired in `main.go`. Handlers return `httpx.Respond(...)`.
- **Internal only: not on the gateway, no auth.** Anyone who can reach it can send mail. Never add a gateway route for `/email`.
- `POST /email` renders and **queues** (row in `emails`); a cron job every 5 s (`Deliver`) claims due rows (`FOR UPDATE SKIP LOCKED`),
  sends, and retries failures with a 30 s doubling back-off up to `EMAIL_MAX_ATTEMPTS`, then marks them `failed`. A row stuck
  `sending` is re-claimed after 2 minutes.
- `type` is a template name (`customer/welcome`, `customer/placeOrder`) looked up by language, falling back to `en`; or
  `no-template` with `data.subject` and `data.body`. A DISABLED template answers `skipped` and queues nothing.
- Templates (`/communications`) use `{{field}}`, `{{%list}}..{{/list}}`, `{{#flag}}..{{/flag}}`, `{{#field Exists}}..{{/field Exists}}`
  (package `render`). Body values are HTML-escaped; a missing field is a 400, so a half-filled email never goes out.
- Subjects are stripped of line breaks, recipients must parse as addresses (max 50): no header injection.
- Without `SMTP_HOST` emails are only logged (and marked sent). 465 = implicit TLS, otherwise STARTTLS. Resend works over SMTP
  (`smtp.resend.com`, user `resend`, password = API key).
- Use a **direct** Postgres host, not a pgbouncer pooler: through Neon's `-pooler` host roughly one request in three failed with
  `unnamed prepared statement does not exist`.
- Never edit an applied migration; add a new key in `migrations.All()`. Each service needs its **own database** (migration versions
  are tracked per database).
