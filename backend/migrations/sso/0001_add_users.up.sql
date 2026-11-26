CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE user_role AS ENUM ('admin', 'user');
CREATE TYPE user_status AS ENUM ('active', 'deactivated');
CREATE TYPE auth_source AS ENUM ('local', 'oidc');

-- SSO holds identity only; Core owns user quotas.
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Stored trimmed and lowercased: a username or an email identifies one
    -- user whatever its case. The unique indexes below are the backstop.
    username VARCHAR(30) NOT NULL,
    email VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255),
    role user_role NOT NULL DEFAULT 'user',
    status user_status NOT NULL DEFAULT 'active',
    auth_source auth_source NOT NULL DEFAULT 'local',
    external_provider VARCHAR(64),
    external_subject VARCHAR(255),
    external_username VARCHAR(255),
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT users_external_identity_complete
        CHECK ((external_provider IS NULL) = (external_subject IS NULL))
);

CREATE UNIQUE INDEX users_username_lower_unique ON users (lower(username));
CREATE UNIQUE INDEX users_email_lower_unique ON users (lower(email));

CREATE UNIQUE INDEX idx_users_external_identity
    ON users (external_provider, external_subject)
    WHERE external_provider IS NOT NULL AND external_subject IS NOT NULL;

CREATE INDEX idx_users_created_at
    ON users (created_at DESC, id DESC);
