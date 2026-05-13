ALTER TABLE user_resource_usage_snapshots
    DROP COLUMN IF EXISTS memory_usage_bytes,
    DROP COLUMN IF EXISTS cpu_percent;
