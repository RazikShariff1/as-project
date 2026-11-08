-- Generic accounts: users, roles, and the endpoint-to-role permissions the gateway checks. Authentication is
-- opaque session tokens, stored only as a hash. No organisations: add organization_id later if tenancy is needed.

CREATE TABLE users (
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
    attributes    JSONB       NOT NULL DEFAULT '{}',          -- free-form, returned to the gateway, e.g. {"sellerId": 12}
    -- audit
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ,
    CONSTRAINT users_status_valid CHECK (status IN ('active', 'suspended')),
    CONSTRAINT users_failed_logins_nonneg CHECK (failed_logins >= 0)
);
CREATE UNIQUE INDEX users_email_idx ON users (email) WHERE deleted_at IS NULL;

CREATE TABLE roles (
    -- identity
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- profile
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    -- access
    all_access  BOOLEAN     NOT NULL DEFAULT FALSE,           -- may call every endpoint, registered or not (admin)
    -- audit
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX roles_name_idx ON roles (name) WHERE deleted_at IS NULL;

CREATE TABLE user_role (
    user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role_id    BIGINT      NOT NULL REFERENCES roles (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role_id)
);
CREATE INDEX user_role_role_idx ON user_role (role_id);

-- An endpoint is something a service exposes: a method and a path pattern such as /product/{id}.
CREATE TABLE endpoints (
    -- identity
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- definition
    service     TEXT        NOT NULL,                         -- e.g. catalogue-svc
    method      TEXT        NOT NULL,
    path        TEXT        NOT NULL,                         -- {name} matches exactly one path segment
    description TEXT        NOT NULL DEFAULT '',
    -- audit
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,
    CONSTRAINT endpoints_method_valid CHECK (method IN ('GET', 'POST', 'PUT', 'PATCH', 'DELETE'))
);
CREATE UNIQUE INDEX endpoints_route_idx ON endpoints (service, method, path) WHERE deleted_at IS NULL;

-- Which roles may call which endpoints.
CREATE TABLE endpoint_role (
    endpoint_id BIGINT      NOT NULL REFERENCES endpoints (id) ON DELETE CASCADE,
    role_id     BIGINT      NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (endpoint_id, role_id)
);
CREATE INDEX endpoint_role_role_idx ON endpoint_role (role_id);

-- A login. The token itself is never stored: only its SHA-256, so a database leak cannot be replayed.
CREATE TABLE sessions (
    -- identity
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash  TEXT        NOT NULL UNIQUE,
    -- client
    user_agent  TEXT        NOT NULL DEFAULT '',
    ip          TEXT        NOT NULL DEFAULT '',
    -- lifetime
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    -- audit
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_idx ON sessions (user_id);
