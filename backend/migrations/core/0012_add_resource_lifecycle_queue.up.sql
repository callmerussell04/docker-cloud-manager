CREATE TABLE resource_lifecycle_queue_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    operation_id UUID NOT NULL REFERENCES resource_operations(id) ON DELETE CASCADE,
    resource_type VARCHAR(32) NOT NULL,
    resource_id UUID NOT NULL,
    exchange VARCHAR(128) NOT NULL,
    routing_key VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT,
    published_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT resource_lifecycle_outbox_operation_id_unique UNIQUE (operation_id)
);

CREATE INDEX idx_resource_lifecycle_outbox_lease
    ON resource_lifecycle_queue_outbox (status, created_at, updated_at)
    WHERE status IN ('pending', 'publishing');

CREATE INDEX idx_resource_lifecycle_outbox_resource
    ON resource_lifecycle_queue_outbox (resource_type, resource_id, created_at DESC);
