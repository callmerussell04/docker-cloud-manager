CREATE MATERIALIZED VIEW IF NOT EXISTS audit_action_daily
ENGINE = SummingMergeTree
PARTITION BY toYYYYMM(day)
ORDER BY (day, action, outcome)
TTL day + INTERVAL 90 DAY
AS SELECT toDate(occurred_at) AS day, action, outcome, count() AS events_count
FROM audit_events
GROUP BY day, action, outcome;
