ALTER TABLE builds
    ADD COLUMN archive_object_key TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_builds_active_status
    ON builds (status, started_at)
    WHERE status IN ('pending', 'running');

CREATE TABLE build_queue_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    exchange VARCHAR(128) NOT NULL,
    routing_key VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    published_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT build_queue_outbox_build_id_unique UNIQUE (build_id)
);

CREATE INDEX idx_build_queue_outbox_pending
    ON build_queue_outbox (created_at)
    WHERE status = 'pending';
