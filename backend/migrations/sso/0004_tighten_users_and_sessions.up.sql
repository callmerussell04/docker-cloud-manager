-- Low-cardinality indexes no query uses.
DROP INDEX idx_users_status;
DROP INDEX idx_users_auth_source;

ALTER TABLE users
    ALTER COLUMN created_at SET NOT NULL,
    ADD CONSTRAINT users_external_identity_complete
        CHECK ((external_provider IS NULL) = (external_subject IS NULL));

ALTER TABLE sessions
    ADD CONSTRAINT sessions_revocation_complete
        CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL));
