-- Column names follow catalogue-service (status ENABLED/HIDDEN, soft delete via deleted_at, client ids, videos,
-- tag_line). Stock, perishable-slot and bulk-order rules are deliberately left out: they belong to inventory/order. Differences: Postgres types, no organization_id, and products are shared by all sellers: a seller offers an existing product through inventory-svc.

CREATE TABLE sellers (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        TEXT        NOT NULL,
    is_house    BOOLEAN     NOT NULL DEFAULT FALSE, -- true for our own storefront (seller #1)
    status      TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- at most one house seller
CREATE UNIQUE INDEX sellers_one_house ON sellers (is_house) WHERE is_house;

CREATE TABLE brands (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        TEXT        NOT NULL,
    slug        TEXT        NOT NULL,
    logo        TEXT        NOT NULL DEFAULT '',
    image       TEXT        NOT NULL DEFAULT '',
    video       TEXT        NOT NULL DEFAULT '',
    description TEXT        NOT NULL DEFAULT '',
    tag_line    TEXT        NOT NULL DEFAULT '',
    status      TEXT        NOT NULL DEFAULT 'ENABLED' CHECK (status IN ('ENABLED', 'HIDDEN')),
    client_id   TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX brands_slug_idx ON brands (slug) WHERE deleted_at IS NULL;
CREATE INDEX brands_name_idx ON brands (name);

CREATE TABLE categories (
    id                      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    parent_category_id      BIGINT REFERENCES categories (id),
    name                    TEXT        NOT NULL,
    slug                    TEXT        NOT NULL,
    image                   TEXT        NOT NULL DEFAULT '',
    video                   TEXT        NOT NULL DEFAULT '',
    description             TEXT        NOT NULL DEFAULT '',
    tag_line                TEXT        NOT NULL DEFAULT '',
    status                  TEXT        NOT NULL DEFAULT 'ENABLED' CHECK (status IN ('ENABLED', 'HIDDEN')),
    client_id               TEXT        NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at              TIMESTAMPTZ
);
CREATE UNIQUE INDEX categories_slug_idx ON categories (slug) WHERE deleted_at IS NULL;
CREATE INDEX categories_parent_idx ON categories (parent_category_id);

CREATE TABLE products (
    id                      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    primary_category_id     BIGINT REFERENCES categories (id),
    brand_id                BIGINT REFERENCES brands (id),
    name                    TEXT        NOT NULL,
    slug                    TEXT        NOT NULL,
    tag_line                TEXT        NOT NULL DEFAULT '',
    description             TEXT        NOT NULL DEFAULT '',
    images                  TEXT[]      NOT NULL DEFAULT '{}',
    videos                  TEXT[]      NOT NULL DEFAULT '{}',
    status                  TEXT        NOT NULL DEFAULT 'ENABLED' CHECK (status IN ('ENABLED', 'HIDDEN')),
    has_variants            BOOLEAN     NOT NULL DEFAULT FALSE,
    sold_by_weight          BOOLEAN     NOT NULL DEFAULT FALSE,
    meta_data               JSONB       NOT NULL DEFAULT '{}',
    client_item_id          TEXT        NOT NULL DEFAULT '',               -- an external id for the product
    barcodes                TEXT[]      NOT NULL DEFAULT '{}',
    search                  TSVECTOR GENERATED ALWAYS AS (
                                to_tsvector('simple', name || ' ' || tag_line || ' ' || description)
                            ) STORED,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at              TIMESTAMPTZ
);
CREATE UNIQUE INDEX products_slug_idx ON products (slug) WHERE deleted_at IS NULL;
CREATE INDEX products_category_idx    ON products (primary_category_id);
CREATE INDEX products_brand_idx       ON products (brand_id);
CREATE INDEX products_client_item_idx ON products (client_item_id, deleted_at);
CREATE INDEX products_barcodes_idx    ON products USING GIN (barcodes);
CREATE INDEX products_search_idx      ON products USING GIN (search);

-- A product can sit in several categories besides its primary one.
CREATE TABLE category_product (
    category_id BIGINT      NOT NULL REFERENCES categories (id),
    product_id  BIGINT      NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (category_id, product_id)
);
CREATE INDEX category_product_product_idx ON category_product (product_id);

-- A variant is the sellable SKU: pure content. Price, discount, tax and stock live in inventory-svc, keyed by this id.
CREATE TABLE variants (
    id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    product_id         BIGINT      NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    client_item_id     TEXT        NOT NULL,                                -- an external id for the variant (SKU)
    name               TEXT        NOT NULL DEFAULT '',
    status             TEXT        NOT NULL DEFAULT 'ENABLED' CHECK (status IN ('ENABLED', 'HIDDEN')),
    barcodes           TEXT[]      NOT NULL DEFAULT '{}',
    images             TEXT[]      NOT NULL DEFAULT '{}',
    videos             TEXT[]      NOT NULL DEFAULT '{}',
    meta_data          JSONB       NOT NULL DEFAULT '{}',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at         TIMESTAMPTZ
);
CREATE UNIQUE INDEX variants_client_item_idx ON variants (product_id, client_item_id) WHERE deleted_at IS NULL;
CREATE INDEX variants_product_idx  ON variants (product_id);
CREATE INDEX variants_barcodes_idx ON variants USING GIN (barcodes);

-- Which sellers may sell which product: an admin-managed whitelist, as in catalogue-service.
CREATE TABLE permissible_product_sellers (
    -- identity
    seller_id   BIGINT      NOT NULL REFERENCES sellers (id),
    product_id  BIGINT      NOT NULL REFERENCES products (id),
    -- state
    status      TEXT        NOT NULL DEFAULT 'ENABLED' CHECK (status IN ('ENABLED', 'DISABLED')),
    -- audit
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (seller_id, product_id)
);
CREATE INDEX permissible_product_sellers_product_idx ON permissible_product_sellers (product_id);

-- A permitted seller asks to sell a product at their own price; an admin approves or rejects. Approving makes the
-- offer live in inventory-svc through an event. At most one request per (seller, product) is APPROVED: approving a
-- new one archives the previous one.
CREATE TABLE seller_product_requests (
    -- identity
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    seller_id        BIGINT      NOT NULL REFERENCES sellers (id),
    product_id       BIGINT      NOT NULL REFERENCES products (id),
    -- workflow
    status           TEXT        NOT NULL DEFAULT 'PENDING',
    rejection_reason TEXT        NOT NULL DEFAULT '',
    -- visibility of the live offer
    product_status   TEXT        NOT NULL DEFAULT 'ENABLED',
    -- when this request's live state was last applied to inventory-svc; null or older than updated_at means not yet
    synced_at        TIMESTAMPTZ,
    -- audit
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    CONSTRAINT seller_product_requests_status_valid CHECK (status IN ('DRAFT', 'PENDING', 'REJECTED', 'APPROVED', 'ARCHIVED')),
    CONSTRAINT seller_product_requests_product_status_valid CHECK (product_status IN ('ENABLED', 'HIDDEN'))
);
CREATE INDEX seller_product_requests_offer_idx ON seller_product_requests (seller_id, product_id);
CREATE INDEX seller_product_requests_unsynced_idx ON seller_product_requests (id) WHERE deleted_at IS NULL AND status IN ('APPROVED', 'ARCHIVED');
CREATE UNIQUE INDEX seller_product_requests_one_approved_idx
    ON seller_product_requests (seller_id, product_id) WHERE status = 'APPROVED' AND deleted_at IS NULL;

-- The variants a request offers, each with the seller's list price (becomes the offer's mrp) and discount.
CREATE TABLE seller_product_items (
    request_id  BIGINT        NOT NULL REFERENCES seller_product_requests (id) ON DELETE CASCADE,
    variant_id  BIGINT        NOT NULL REFERENCES variants (id),
    mrp         NUMERIC(11,5) NOT NULL,
    discount    NUMERIC(11,5) NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    PRIMARY KEY (request_id, variant_id),
    CONSTRAINT seller_product_items_mrp_nonneg CHECK (mrp >= 0),
    CONSTRAINT seller_product_items_discount_within_mrp CHECK (discount >= 0 AND discount <= mrp)
);
CREATE INDEX seller_product_items_variant_idx ON seller_product_items (variant_id);
