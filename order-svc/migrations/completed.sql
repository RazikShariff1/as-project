-- completed_at: when the order became COMPLETED, for the completedBetween filter (as in the reference service).
ALTER TABLE orders ADD COLUMN completed_at TIMESTAMPTZ;

UPDATE orders o SET completed_at = (
    SELECT max(l.created_at) FROM order_logs l WHERE l.order_id = o.id AND l.action = 'STATUS' AND l.details ->> 'to' = 'COMPLETED')
 WHERE o.status = 'COMPLETED';

CREATE INDEX orders_completed_idx ON orders (completed_at) WHERE completed_at IS NOT NULL;
-- sellerID filter: orders containing a seller's items.
CREATE INDEX order_items_seller_idx ON order_items (seller_id, order_id);
