# catalogue-svc

Source of truth for **what is sold** on the Zopping marketplace platform: brands, categories, products and variants (SKUs),
plus **which sellers may sell what** and the approval workflow that puts a seller's offer on sale.

Products are **shared content**. A product has no owner: it has one set of images, descriptions and details, and many
sellers can sell it, each with their own stock and price. Those, and the offers themselves, live in
[inventory-svc](../inventory-svc), keyed by variant and seller. This service decides who is allowed to offer what.

Built in Go on [gofr.dev](https://gofr.dev) with Postgres, following the conventions of `catalogue-service` and
`inventory-service`.

## Quick start

```bash
make create-db       # once: creates the "catalogue" database in the shared postgres-root container
docker start pubsub-emulator   # the shared Pub/Sub emulator (project test-project, port 8085)
make run             # GOFR_ENV=local, port 8081; migrations run on startup
curl localhost:8081/.well-known/health
```

## API

Paths are singular. Lists take `limit` (default 20, max 100), `page` and `offset`. Responses are wrapped by GoFr as
`{"data": ...}`. Full shapes are in [api/openapi.json](api/openapi.json). IDs are integers.

| Resource | Routes | List filters |
|---|---|---|
| Seller | `GET/POST /seller`, `GET/PUT /seller/{id}` | `status` |
| Brand | `GET/POST /brand`, `GET/PUT/DELETE /brand/{id}` | `name` (prefix), `slug`, `clientId`, `status` |
| Category | `GET/POST /category`, `GET/PUT/DELETE /category/{id}` | `parentCategoryId`, `slug`, `clientId`, `status` |
| Product | `GET/POST /product`, `GET/PUT/DELETE /product/{id}` | `categoryId`, `brandId`, `status`, `clientItemId`, `barcode`, `q` (search) |
| Variant | `GET/POST /variant`, `GET/PUT/DELETE /variant/{id}` | `productId`, `clientItemId`, `barcode`, `status` |
| Permissible sellers | `PUT /product/{id}/permissible-seller`, `GET /permissible-seller` | `sellerId`, `productId`, `status` |
| Seller product request | `GET/POST /seller-product-request`, `GET/PUT/DELETE /seller-product-request/{id}`, `PUT .../{id}/approval`, `PUT .../{id}/visibility`, `PUT /seller-product-request/reconcile` | `sellerId`, `productId`, `status` |

- Lists (except sellers) return `{brands|categories|products|variants|..., count, offset, limit}`. A product includes its variants.
- Status is `ENABLED` or `HIDDEN` (sellers use `active`/`suspended`). `DELETE` is a soft delete; deleting a product also deletes its variants.
- A blank `slug` is generated from the name. A product has a `primaryCategoryId` and a `categoryIds` list (the primary is always in it).
- A variant is content only. Price, tax and stock are in inventory-svc.

## How a seller sells a product

This follows `catalogue-service`'s multi-seller flow.

1. **Permit.** An admin lists which sellers may sell a product: `PUT /product/{id}/permissible-seller` with `{"sellerIds": [1, 2]}`.
   The listed sellers become `ENABLED`, any other seller listed before becomes `DISABLED`.
2. **Request.** A permitted, active seller files `POST /seller-product-request` with `{sellerId, productId, items: [{variantId, mrp, discount}]}`
   (status `DRAFT` or `PENDING`, default `PENDING`). `mrp` is the seller's list price; the selling price is `mrp - discount`. Items
   must be live variants of that product, each once, with `0 <= discount <= mrp`.
3. **Review.** An admin calls `PUT .../{id}/approval` with `{"status": "APPROVED"}`, or `{"status": "REJECTED", "rejectionReason": "..."}`
   (a reason is required). A rejected or draft request can be revised and resubmitted with `PUT .../{id}`.
4. **Live.** Approving publishes a change event. This service subscribes to it and calls inventory-svc to create or update the seller's
   offer for each variant (never touching stock). Normally this takes a couple of seconds.
5. **Manage.** `PUT .../{id}/visibility` with `{"productStatus": "HIDDEN"}` hides the live offer and `ENABLED` shows it again.

Statuses: `DRAFT`, `PENDING`, `REJECTED`, `APPROVED`, `ARCHIVED`. A seller has at most **one approved request per product**, and it defines
their whole offering for it: approving a new one archives the previous one, and any variant the new one no longer lists is withdrawn
from sale. An approved request cannot be edited or deleted; file a new one instead. A suspended seller cannot file or be approved, and a
seller removed from the whitelist cannot file or resubmit.

### Keeping inventory in step

Each request has `synced`: `false` while an `APPROVED` or `ARCHIVED` request has a change inventory-svc has not seen yet.

**A scheduler must call `PUT /seller-product-request/reconcile` regularly (every minute is plenty).** It applies anything unsynced and
returns `{"applied": n}`; it is safe to repeat. Without it, an approval that happened while inventory-svc was down never goes live.
The event is only a trigger. The database is the source of truth, and reconcile repairs a lost, failed or unsent event. Approving an
already-approved request also re-sends the event.

## Configuration

`configs/.env` holds the shared settings and `configs/.local.env` the local ones (selected by `GOFR_ENV`):

| Key | Purpose |
|---|---|
| `PUBSUB_BACKEND`, `GOOGLE_PROJECT_ID`, `GOOGLE_SUBSCRIPTION_NAME`, `PUBSUB_EMULATOR_HOST` | Pub/Sub. Locally this is the `pubsub-emulator` container on `localhost:8085` |
| `SELLER_PRODUCT_CHANGE_TOPIC` | Topic for seller product change events (default `seller-product-change`) |
| `SELLER_CHANGE_DELIVERY` | `direct` applies approvals to inventory-svc in-process, with no Pub/Sub (single-container deploys). Default `pubsub` |
| `INVENTORY_SERVICE_URL` | Where inventory-svc is (default `http://localhost:8082`) |

There is no service-level auth: the gateway validates every request, so this service must only be reachable from inside the platform
network.

## Docs

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): layers, data model, the seller flow and the event design
- [api/openapi.json](api/openapi.json): request and response shapes
- [CLAUDE.md](CLAUDE.md): guidance for coding agents
