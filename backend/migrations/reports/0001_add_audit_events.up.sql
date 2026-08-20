CREATE TABLE IF NOT EXISTS audit_events (
    id UUID,
    occurred_at DateTime64(3, 'UTC'),
    actor_user_id UUID,
    actor_username String,
    actor_scope LowCardinality(String),
    action LowCardinality(String),
    outcome LowCardinality(String),
    resource_type LowCardinality(String),
    resource_id String,
    resource_name String,
    owner_id UUID,
    owner_username String,
    request_id String,
    client_ip String,
    user_agent String,
    error_code String,
    details_json String
) ENGINE = MergeTree
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (occurred_at, actor_user_id, action, resource_type)
TTL toDateTime(occurred_at) + INTERVAL 90 DAY;
