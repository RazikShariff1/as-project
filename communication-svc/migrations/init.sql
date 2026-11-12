-- Email templates, keyed like the reference communication-service: a communication name ("customer/welcome") per language.
-- The body is HTML using {{field}}, {{%list}}...{{/list}} and {{#flag}}...{{/flag}}.
CREATE TABLE templates (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT        NOT NULL,
    language   TEXT        NOT NULL DEFAULT 'en',
    subject    TEXT        NOT NULL,
    body       TEXT        NOT NULL,
    status     TEXT        NOT NULL DEFAULT 'ENABLED',       -- a DISABLED template skips its emails
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT templates_status_valid CHECK (status IN ('ENABLED', 'DISABLED')),
    CONSTRAINT templates_name_language_key UNIQUE (name, language)
);

-- Every email accepted by POST /email: the outbox the delivery worker drains, and the log of what became of each one.
CREATE TABLE emails (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    type         TEXT        NOT NULL,                        -- the template name, or "no-template"
    language     TEXT        NOT NULL DEFAULT 'en',
    to_addr      TEXT        NOT NULL,
    cc_addr      TEXT        NOT NULL DEFAULT '',
    bcc_addr     TEXT        NOT NULL DEFAULT '',
    subject      TEXT        NOT NULL,
    body         TEXT        NOT NULL,
    status       TEXT        NOT NULL DEFAULT 'queued',
    attempts     INT         NOT NULL DEFAULT 0,
    last_error   TEXT        NOT NULL DEFAULT '',
    send_after   TIMESTAMPTZ NOT NULL DEFAULT now(),          -- not before this (retry back-off)
    locked_until TIMESTAMPTZ,                                 -- a worker holds it until then; a crashed one's rows are re-claimed
    sent_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT emails_status_valid CHECK (status IN ('queued', 'sending', 'sent', 'failed'))
);
CREATE INDEX emails_due_idx ON emails (send_after) WHERE status IN ('queued', 'sending');
CREATE INDEX emails_created_idx ON emails (created_at DESC);

INSERT INTO templates (name, language, subject, body) VALUES
('customer/welcome', 'en', 'Welcome, {{name}}!',
 '<p>Hi {{name}},</p><p>Your account is ready. Happy shopping!</p>'),
('customer/placeOrder', 'en', 'We have received your order {{orderRef}}',
 '<p>Hi {{name}},</p><p>Thank you for your order <b>{{orderRef}}</b>.</p><table>{{%items}}<tr><td>{{name}}</td><td>{{quantity}}</td><td>{{price}}</td></tr>{{/items}}</table><p>Total: <b>{{total}}</b></p>');
