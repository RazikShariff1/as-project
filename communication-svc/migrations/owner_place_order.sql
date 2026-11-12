-- The email the owner (admin) gets when an order is placed, as in the reference service's owner/placeOrder.
INSERT INTO templates (name, language, subject, body) VALUES
('owner/placeOrder', 'en', 'New order {{orderRef}}',
 '<p>A new order <b>{{orderRef}}</b> (#{{referenceNumber}}) was placed by customer {{customerId}}.</p><table>{{%items}}<tr><td>variant {{variantId}}</td><td>x {{quantity}}</td><td>{{unitPrice}}</td><td>{{lineTotal}}</td></tr>{{/items}}</table><p>Payment: {{paymentMethod}}<br/>Total: <b>{{total}}</b></p>')
ON CONFLICT (name, language) DO NOTHING;
