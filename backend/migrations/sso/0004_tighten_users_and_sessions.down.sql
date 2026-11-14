ALTER TABLE sessions DROP CONSTRAINT sessions_revocation_complete;

ALTER TABLE users
    DROP CONSTRAINT users_external_identity_complete,
    ALTER COLUMN created_at DROP NOT NULL;

CREATE INDEX idx_users_auth_source
    ON users (auth_source);

CREATE INDEX idx_users_status
    ON users (status);
