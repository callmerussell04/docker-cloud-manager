CREATE MATERIALIZED VIEW IF NOT EXISTS user_usage_daily
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(day)
ORDER BY (day, owner_id)
TTL day + INTERVAL 90 DAY
AS SELECT toDate(bucket_start) AS day, owner_id, anyLastState(owner_username) AS owner_username_state, maxState(reserved_memory_bytes) AS reserved_memory_bytes_state, maxState(total_disk_bytes) AS total_disk_bytes_state, maxState(containers_total) AS containers_total_state, maxState(containers_running) AS containers_running_state, maxState(volumes_total) AS volumes_total_state, maxState(images_total) AS images_total_state, maxState(builds_total) AS builds_total_state, maxState(projects_total) AS projects_total_state
FROM user_resource_usage_snapshots
GROUP BY day, owner_id;
