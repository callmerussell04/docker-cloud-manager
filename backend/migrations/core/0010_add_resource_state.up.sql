ALTER TABLE containers
    ADD COLUMN desired_status VARCHAR(20) NOT NULL DEFAULT 'created',
    ADD COLUMN last_observed_at TIMESTAMP WITH TIME ZONE,
    ADD COLUMN last_error TEXT,
    ADD COLUMN docker_generation INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN network_alias VARCHAR(100) DEFAULT '',
    ADD COLUMN command JSONB,
    ADD COLUMN entrypoint JSONB,
    ADD COLUMN restart_policy VARCHAR(32) DEFAULT '',
    ADD COLUMN healthcheck JSONB;

UPDATE containers
SET desired_status = status
WHERE desired_status = 'created';

ALTER TABLE volumes
    ADD COLUMN status VARCHAR(20) NOT NULL DEFAULT 'available',
    ADD COLUMN last_observed_at TIMESTAMP WITH TIME ZONE,
    ADD COLUMN last_error TEXT;

ALTER TABLE images
    ADD COLUMN status VARCHAR(20) NOT NULL DEFAULT 'available',
    ADD COLUMN last_observed_at TIMESTAMP WITH TIME ZONE,
    ADD COLUMN last_error TEXT;

CREATE TABLE resource_operations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    resource_type VARCHAR(32) NOT NULL,
    resource_id UUID NOT NULL,
    owner_id UUID NOT NULL,
    operation VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX idx_resource_operations_active
    ON resource_operations (resource_type, resource_id)
    WHERE status IN ('pending', 'running');

CREATE INDEX idx_resource_operations_resource
    ON resource_operations (resource_type, resource_id, created_at DESC);
