CREATE TABLE IF NOT EXISTS user_resource_usage_hourly (
    owner_id UUID,
    owner_username String,
    bucket_start DateTime('UTC'),
    collected_at DateTime64(3, 'UTC'),
    reserved_memory_bytes Int64,
    image_disk_bytes Int64,
    volume_disk_bytes Int64,
    total_disk_bytes Int64,
    containers_total UInt32,
    containers_running UInt32,
    volumes_total UInt32,
    images_total UInt32,
    builds_total UInt32,
    projects_total UInt32
) ENGINE = ReplacingMergeTree(collected_at)
PARTITION BY toYYYYMM(bucket_start)
ORDER BY (owner_id, bucket_start)
TTL bucket_start + INTERVAL 90 DAY;
