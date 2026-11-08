-- An order placed by a customer. customer_id is an account-svc users.id and order_ref the reference used with inventory-svc's
-- reservations; neither is a cross-service foreign key. Amounts are rupees (NUMERIC(11,5), like inventory-svc prices).
-- Lifecycle: PLACED -> CONFIRMED -> PACKED -> OUT_FOR_DELIVERY -> DELIVERED, or CANCELLED from PLACED/CONFIRMED.
-- Stock stays reserved until PACKED (then it is confirmed as a sale) and is released on cancel.

CREATE TABLE orders (
    id               BIGSERIAL PRIMARY KEY,
    order_ref        TEXT          NOT NULL UNIQUE,
    customer_id      BIGINT        NOT NULL,
    status           TEXT          NOT NULL DEFAULT 'PLACED',
    payment_method   TEXT          NOT NULL DEFAULT 'COD',
    payment_status   TEXT          NOT NULL DEFAULT 'PENDING',
    subtotal         NUMERIC(11,5) NOT NULL,                -- sum of mrp x quantity
    discount_total   NUMERIC(11,5) NOT NULL DEFAULT 0,      -- sum of discount x quantity
    total            NUMERIC(11,5) NOT NULL,                -- subtotal - discount_total
    delivery_address JSONB         NOT NULL,
    notes            TEXT          NOT NULL DEFAULT '',
    cancel_reason    TEXT          NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ   NOT NULL DEFAULT now(),
    CONSTRAINT orders_status_valid CHECK (status IN ('PLACED', 'CONFIRMED', 'PACKED', 'OUT_FOR_DELIVERY', 'DELIVERED', 'CANCELLED')),
    CONSTRAINT orders_payment_method_valid CHECK (payment_method IN ('COD')),
    CONSTRAINT orders_payment_status_valid CHECK (payment_status IN ('PENDING', 'PAID', 'FAILED', 'REFUNDED')),
    CONSTRAINT orders_amounts_nonneg CHECK (subtotal >= 0 AND discount_total >= 0 AND total >= 0)
);

CREATE INDEX orders_customer_idx ON orders (customer_id, created_at DESC);
CREATE INDEX orders_status_idx ON orders (status, created_at DESC);

-- What was bought, with the price frozen at order time (later price changes never touch it). quantity is in selling units;
-- unit_price is the selling price (mrp - discount) per selling unit.
CREATE TABLE order_items (
    id         BIGSERIAL PRIMARY KEY,
    order_id   BIGINT        NOT NULL REFERENCES orders (id),
    variant_id BIGINT        NOT NULL,
    seller_id  BIGINT        NOT NULL,
    quantity   BIGINT        NOT NULL,
    mrp        NUMERIC(11,5) NOT NULL,
    discount   NUMERIC(11,5) NOT NULL DEFAULT 0,
    unit_price NUMERIC(11,5) NOT NULL,
    line_total NUMERIC(11,5) NOT NULL,
    tax        TEXT          NOT NULL DEFAULT '',
    hsn_code   TEXT          NOT NULL DEFAULT '',
    UNIQUE (order_id, variant_id, seller_id),
    CONSTRAINT order_items_quantity_positive CHECK (quantity >= 1)
);

-- Append-only history of status and payment-status changes.
CREATE TABLE order_logs (
    id          BIGSERIAL PRIMARY KEY,
    order_id    BIGINT      NOT NULL REFERENCES orders (id),
    kind        TEXT        NOT NULL,   -- STATUS or PAYMENT
    from_value  TEXT        NOT NULL DEFAULT '',
    to_value    TEXT        NOT NULL,
    note        TEXT        NOT NULL DEFAULT '',
    user_id     BIGINT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX order_logs_order_idx ON order_logs (order_id, id);
