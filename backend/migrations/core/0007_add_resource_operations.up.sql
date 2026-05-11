CREATE TABLE resource_operations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    resource_type VARCHAR(32) NOT NULL,
    resource_id UUID NOT NULL,
    owner_id UUID NOT NULL,
    operation VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX idx_resource_operations_active
    ON resource_operations (resource_type, resource_id)
    WHERE status IN ('pending', 'running');

CREATE INDEX idx_resource_operations_resource
    ON resource_operations (resource_type, resource_id, created_at DESC);

