-- One row per (variant, seller): that seller's offer for a shared catalogue product. variant_id is a catalogue-svc
-- variants.id and seller_id a seller id (no cross-service foreign keys). Each seller has their own quantity, price and
-- reservations for the same variant, as inventory-service keys items by product and seller. Available = quantity - reserved.
-- Like inventory-service's items table, the row also carries the commercial details that change often: mrp, discount,
-- tax, hsn_code (and our marketplace addition, cost_price). The catalogue holds content only.
-- Every quantity (quantity, reserved, deltas, reservations) is in base units; unit says how many make one selling unit.
-- A HIDDEN row is not sellable: it reports nothing available and rejects new reservations.

CREATE TABLE stock (
    -- identity
    variant_id          BIGINT      NOT NULL,
    seller_id           BIGINT      NOT NULL,
    -- stock levels (base units)
    quantity            BIGINT      NOT NULL DEFAULT 0,
    reserved            BIGINT      NOT NULL DEFAULT 0,
    unlimited_stock     BOOLEAN     NOT NULL DEFAULT FALSE,   -- e.g. made-to-order: never runs out, never decremented
    low_stock_threshold BIGINT,
    unit                INT         NOT NULL DEFAULT 1,       -- base units per selling unit (1 = counted in pieces; 1000 = kg held in grams)
    -- visibility
    status              TEXT        NOT NULL DEFAULT 'ENABLED',
    -- commercial details (rupees)
    mrp                 NUMERIC(11,5),                          -- list price; null until a price is set
    discount            NUMERIC(11,5) NOT NULL DEFAULT 0,       -- amount off mrp: selling price = mrp - discount
    cost_price          NUMERIC(11,5),                          -- what we owe the seller per unit; never shown to the storefront
    tax                 TEXT        NOT NULL DEFAULT '',
    hsn_code            TEXT        NOT NULL DEFAULT '',
    -- audit
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (variant_id, seller_id),
    CONSTRAINT stock_quantity_nonneg CHECK (quantity >= 0),
    CONSTRAINT stock_reserved_nonneg CHECK (reserved >= 0),
    CONSTRAINT stock_reserved_within_quantity CHECK (unlimited_stock OR reserved <= quantity),
    CONSTRAINT stock_unit_positive CHECK (unit >= 1),
    CONSTRAINT stock_status_valid CHECK (status IN ('ENABLED', 'HIDDEN')),
    CONSTRAINT stock_mrp_nonneg CHECK (mrp IS NULL OR mrp >= 0),
    CONSTRAINT stock_discount_nonneg CHECK (discount >= 0),
    CONSTRAINT stock_cost_price_nonneg CHECK (cost_price IS NULL OR cost_price >= 0),
    CONSTRAINT stock_discount_within_mrp CHECK (discount = 0 OR (mrp IS NOT NULL AND discount <= mrp))
);

CREATE INDEX stock_seller_idx ON stock (seller_id);

-- Audit trail of every quantity change. A (reference_type, reference_id, variant_id, seller_id, reason) combination is recorded
-- once, which makes a retried receipt or sale a rejected duplicate instead of a double count.
CREATE TABLE stock_movements (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    variant_id      BIGINT      NOT NULL,
    seller_id       BIGINT      NOT NULL,
    delta           BIGINT      NOT NULL,
    quantity_after  BIGINT      NOT NULL,
    reason          TEXT        NOT NULL,
    notes           TEXT        NOT NULL DEFAULT '',
    user_id         BIGINT,                                  -- who made the change; null for system changes such as a sale
    reference_type  TEXT        NOT NULL DEFAULT '',
    reference_id    TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT stock_movements_reason_valid CHECK (reason IN
        ('RECEIVED', 'RETURNED_TO_SELLER', 'EXPIRED', 'DAMAGED', 'RETURNED', 'SOLD', 'RECOUNTED', 'OTHER'))
);
CREATE INDEX stock_movements_offer_idx ON stock_movements (variant_id, seller_id, id DESC);
CREATE UNIQUE INDEX stock_movements_reference_idx
    ON stock_movements (reference_type, reference_id, variant_id, seller_id, reason) WHERE reference_id <> '';

-- A hold on stock for an order while it is being paid for: RESERVED -> CONFIRMED (stock leaves) or RELEASED.
CREATE TABLE stock_reservations (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_ref   TEXT        NOT NULL,
    variant_id  BIGINT      NOT NULL,
    seller_id   BIGINT      NOT NULL,
    quantity    BIGINT      NOT NULL CHECK (quantity > 0),
    status      TEXT        NOT NULL DEFAULT 'RESERVED' CHECK (status IN ('RESERVED', 'CONFIRMED', 'RELEASED')),
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (order_ref, variant_id, seller_id),
    FOREIGN KEY (variant_id, seller_id) REFERENCES stock (variant_id, seller_id)
);
CREATE INDEX stock_reservations_expiry_idx ON stock_reservations (expires_at) WHERE status = 'RESERVED';

-- Audit trail of price, discount, tax, HSN and cost changes (the reference logs these separately from stock).
CREATE TABLE stock_price_log (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    variant_id  BIGINT      NOT NULL,
    seller_id   BIGINT      NOT NULL,
    field       TEXT        NOT NULL CHECK (field IN ('MRP', 'DISCOUNT', 'COST_PRICE', 'TAX', 'HSN_CODE')),
    old_value   TEXT,
    new_value   TEXT,
    notes       TEXT        NOT NULL DEFAULT '',
    user_id     BIGINT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX stock_price_log_offer_idx ON stock_price_log (variant_id, seller_id, id DESC);
