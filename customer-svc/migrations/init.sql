-- Shoppers: their profile, saved addresses and login sessions. Separate from account-svc, which holds the team.
-- Authentication is opaque session tokens, stored only as a hash.

CREATE TABLE customers (
    -- identity
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- profile
    name          TEXT        NOT NULL,
    email         TEXT        NOT NULL,                       -- stored lower-case
    phone         TEXT        NOT NULL DEFAULT '',
    -- credentials
    password_hash TEXT        NOT NULL,
    failed_logins INT         NOT NULL DEFAULT 0,             -- consecutive failures; reset on success
    locked_until  TIMESTAMPTZ,                                -- no login attempts accepted until then
    last_login_at TIMESTAMPTZ,
    -- state
    status        TEXT        NOT NULL DEFAULT 'active',
    -- audit
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ,
    CONSTRAINT customers_status_valid CHECK (status IN ('active', 'suspended')),
    CONSTRAINT customers_failed_logins_nonneg CHECK (failed_logins >= 0)
);
CREATE UNIQUE INDEX customers_email_idx ON customers (email) WHERE deleted_at IS NULL;

CREATE TABLE addresses (
    -- identity
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id BIGINT      NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    -- address
    label       TEXT        NOT NULL DEFAULT '',              -- e.g. Home, Office
    recipient   TEXT        NOT NULL,
    phone       TEXT        NOT NULL DEFAULT '',
    line1       TEXT        NOT NULL,
    line2       TEXT        NOT NULL DEFAULT '',
    city        TEXT        NOT NULL,
    state       TEXT        NOT NULL DEFAULT '',
    postal_code TEXT        NOT NULL,
    country     TEXT        NOT NULL DEFAULT '',
    is_default  BOOLEAN     NOT NULL DEFAULT FALSE,
    -- audit
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE INDEX addresses_customer_idx ON addresses (customer_id) WHERE deleted_at IS NULL;
-- At most one default address per customer, enforced by the database.
CREATE UNIQUE INDEX addresses_default_idx ON addresses (customer_id) WHERE is_default AND deleted_at IS NULL;

-- A login. The token itself is never stored: only its SHA-256, so a database leak cannot be replayed.
CREATE TABLE sessions (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id BIGINT      NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    token_hash  TEXT        NOT NULL UNIQUE,
    user_agent  TEXT        NOT NULL DEFAULT '',
    ip          TEXT        NOT NULL DEFAULT '',
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sessions_customer_idx ON sessions (customer_id);
