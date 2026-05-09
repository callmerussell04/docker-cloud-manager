CREATE TYPE auth_source AS ENUM ('local', 'oidc');

ALTER TABLE users
    ALTER COLUMN password_hash DROP NOT NULL,
    ADD COLUMN auth_source auth_source NOT NULL DEFAULT 'local',
    ADD COLUMN external_provider VARCHAR(64),
    ADD COLUMN external_subject VARCHAR(255),
    ADD COLUMN external_username VARCHAR(255),
    ADD COLUMN last_login_at TIMESTAMP WITH TIME ZONE;

CREATE UNIQUE INDEX idx_users_external_identity
    ON users(external_provider, external_subject)
    WHERE external_provider IS NOT NULL AND external_subject IS NOT NULL;

CREATE INDEX idx_users_auth_source ON users(auth_source);
