ALTER TABLE user_resource_usage_snapshots
    ADD COLUMN IF NOT EXISTS memory_usage_bytes Int64 DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cpu_percent Float64 DEFAULT 0;
