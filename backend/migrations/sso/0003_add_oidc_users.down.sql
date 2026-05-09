DROP INDEX IF EXISTS idx_users_auth_source;
DROP INDEX IF EXISTS idx_users_external_identity;

ALTER TABLE users
    DROP COLUMN IF EXISTS last_login_at,
    DROP COLUMN IF EXISTS external_username,
    DROP COLUMN IF EXISTS external_subject,
    DROP COLUMN IF EXISTS external_provider,
    DROP COLUMN IF EXISTS auth_source,
    ALTER COLUMN password_hash SET NOT NULL;

DROP TYPE IF EXISTS auth_source;
