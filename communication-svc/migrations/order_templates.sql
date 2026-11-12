-- The order emails, worded as in the reference communication-service. {{invoice}} is the order's items, totals and delivery
-- address as HTML, built by order-svc. The owner and customer templates differ only in who they greet and what they say.
UPDATE templates SET
    subject = '[{{organizationName}}] Order Confirmation [Order ID: #{{orderNumber}}]',
    body = $b$<div style="max-width:600px;font-family:sans-serif;font-size:14px;letter-spacing:0.2px;color:#000000;"><p style="font-weight:800;margin:0;">Dear {{customerName}},</p><br/><div style="color:#40A416;"><p style="margin:0;">Thank you for placing your order at {{organizationName}}. Your order is confirmed. Here are the details of your order:</p></div><p>{{invoice}}</p>{{#supportEmail Exists}}<p>In case you have any queries or specific feedback for us, please email us at {{supportEmail}}</p>{{/supportEmail Exists}}</div>$b$,
    updated_at = now()
WHERE name = 'customer/placeOrder' AND language = 'en';

UPDATE templates SET
    subject = '[{{organizationName}}] Order Received [Order ID: #{{orderNumber}}]',
    body = $b$<div style="max-width:600px;font-family:sans-serif;font-size:14px;letter-spacing:0.2px;color:#000000;"><p style="font-weight:800;margin:0;">Dear {{ownerName}},</p><br/><div style="color:#40A416;"><p style="margin:0;">An order has been received successfully from your website. Order details are as follows:</p></div><p>{{invoice}}</p></div>$b$,
    updated_at = now()
WHERE name = 'owner/placeOrder' AND language = 'en';
