-- Align with the reference order-service: statuses PENDING -> PACKED -> DISPATCHED -> COMPLETED (or CANCELLED from PENDING; no
-- in-store processing statuses), payment statuses PENDING/PAID/CANCELLED/REFUND, idempotent placing through client_id, free-form
-- meta_data, and the per-line unit and status that let a pending order's items be edited.

ALTER TABLE orders DROP CONSTRAINT orders_status_valid;
ALTER TABLE orders DROP CONSTRAINT orders_payment_status_valid;

UPDATE orders SET status = CASE status
    WHEN 'PLACED' THEN 'PENDING' WHEN 'CONFIRMED' THEN 'PENDING'
    WHEN 'OUT_FOR_DELIVERY' THEN 'DISPATCHED' WHEN 'DELIVERED' THEN 'COMPLETED' ELSE status END;
UPDATE orders SET payment_status = CASE payment_status WHEN 'FAILED' THEN 'CANCELLED' WHEN 'REFUNDED' THEN 'REFUND' ELSE payment_status END;

UPDATE order_logs SET
    from_value = CASE from_value WHEN 'PLACED' THEN 'PENDING' WHEN 'CONFIRMED' THEN 'PENDING'
        WHEN 'OUT_FOR_DELIVERY' THEN 'DISPATCHED' WHEN 'DELIVERED' THEN 'COMPLETED' ELSE from_value END,
    to_value = CASE to_value WHEN 'PLACED' THEN 'PENDING' WHEN 'CONFIRMED' THEN 'PENDING'
        WHEN 'OUT_FOR_DELIVERY' THEN 'DISPATCHED' WHEN 'DELIVERED' THEN 'COMPLETED' ELSE to_value END
 WHERE kind = 'STATUS';
UPDATE order_logs SET
    from_value = CASE from_value WHEN 'FAILED' THEN 'CANCELLED' WHEN 'REFUNDED' THEN 'REFUND' ELSE from_value END,
    to_value = CASE to_value WHEN 'FAILED' THEN 'CANCELLED' WHEN 'REFUNDED' THEN 'REFUND' ELSE to_value END
 WHERE kind = 'PAYMENT';

ALTER TABLE orders ALTER COLUMN status SET DEFAULT 'PENDING';
ALTER TABLE orders ADD CONSTRAINT orders_status_valid
    CHECK (status IN ('PENDING', 'PACKED', 'DISPATCHED', 'COMPLETED', 'CANCELLED'));
ALTER TABLE orders ADD CONSTRAINT orders_payment_status_valid
    CHECK (payment_status IN ('PENDING', 'PAID', 'CANCELLED', 'REFUND'));

-- client_id: the caller's own id for the order; one per customer. NULL when the caller sent none.
-- stale_refs: reservations an item edit replaced, not yet released in inventory-svc.
ALTER TABLE orders ADD COLUMN client_id  TEXT;
ALTER TABLE orders ADD COLUMN meta_data  JSONB  NOT NULL DEFAULT '{}';
ALTER TABLE orders ADD COLUMN stale_refs TEXT[] NOT NULL DEFAULT '{}';
CREATE UNIQUE INDEX orders_client_id_uniq ON orders (customer_id, client_id) WHERE client_id IS NOT NULL;

-- The staff list is newest-first across all customers.
CREATE INDEX orders_created_idx ON orders (created_at DESC, id DESC);

-- unit: base units per selling unit, frozen with the price (rows from before this change assume 1).
ALTER TABLE order_items ADD COLUMN unit   INT  NOT NULL DEFAULT 1;
ALTER TABLE order_items ADD COLUMN status TEXT NOT NULL DEFAULT 'PENDING';
ALTER TABLE order_items ADD CONSTRAINT order_items_status_valid CHECK (status IN ('PENDING', 'CANCELLED'));
ALTER TABLE order_items ADD CONSTRAINT order_items_unit_positive CHECK (unit >= 1);
