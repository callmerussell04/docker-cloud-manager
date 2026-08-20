CREATE TABLE container_lifecycle_queue_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    operation_id UUID NOT NULL REFERENCES resource_operations(id) ON DELETE CASCADE,
    container_id UUID NOT NULL REFERENCES containers(id) ON DELETE CASCADE,
    exchange VARCHAR(128) NOT NULL,
    routing_key VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT,
    published_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT container_lifecycle_outbox_operation_id_unique UNIQUE (operation_id)
);

CREATE INDEX idx_container_lifecycle_outbox_lease
    ON container_lifecycle_queue_outbox (status, created_at, updated_at)
    WHERE status IN ('pending', 'publishing');

ALTER TABLE compose_deployment_jobs
    ADD COLUMN stage VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN plan_json JSONB,
    ADD COLUMN resource_map_json JSONB;

CREATE INDEX idx_compose_deployment_jobs_stage
    ON compose_deployment_jobs (stage, updated_at)
    WHERE status IN ('running', 'canceling');
