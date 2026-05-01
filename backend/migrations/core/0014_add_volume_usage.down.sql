DROP INDEX IF EXISTS idx_volumes_owner_usage;

ALTER TABLE volumes
    DROP COLUMN IF EXISTS usage_observed_at,
    DROP COLUMN IF EXISTS used_bytes;
