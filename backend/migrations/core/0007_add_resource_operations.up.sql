-- A queued operation on a container, volume or image. resource_id has no
-- foreign key: it names a row of the table resource_type selects, and a delete
-- operation outlives its resource. Finished operations are removed by the
-- retention worker.
CREATE TABLE resource_operations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    resource_type VARCHAR(32) NOT NULL CHECK (resource_type IN (
        'container', 'volume', 'image', 'project', 'build')),
    resource_id UUID NOT NULL,
    owner_id UUID NOT NULL,
    operation VARCHAR(32) NOT NULL CHECK (operation IN (
        'create', 'start', 'stop', 'delete', 'expose')),
    status VARCHAR(32) NOT NULL CHECK (status IN (
        'pending', 'running', 'done', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- A resource has at most one pending or running operation.
CREATE UNIQUE INDEX idx_resource_operations_active
    ON resource_operations (resource_type, resource_id)
    WHERE status IN ('pending', 'running');

CREATE INDEX idx_resource_operations_resource
    ON resource_operations (resource_type, resource_id, created_at DESC);

CREATE TABLE container_lifecycle_queue_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    operation_id UUID NOT NULL REFERENCES resource_operations(id) ON DELETE CASCADE,
    container_id UUID NOT NULL REFERENCES containers(id) ON DELETE CASCADE,
    exchange VARCHAR(128) NOT NULL,
    routing_key VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending' CHECK (status IN (
        'pending', 'publishing', 'published', 'discarded')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT container_lifecycle_outbox_operation_id_unique UNIQUE (operation_id)
);

CREATE INDEX idx_container_lifecycle_outbox_lease
    ON container_lifecycle_queue_outbox (status, created_at, updated_at)
    WHERE status IN ('pending', 'publishing');

-- The outbox of volume and image operations.
CREATE TABLE resource_lifecycle_queue_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    operation_id UUID NOT NULL REFERENCES resource_operations(id) ON DELETE CASCADE,
    resource_type VARCHAR(32) NOT NULL,
    resource_id UUID NOT NULL,
    exchange VARCHAR(128) NOT NULL,
    routing_key VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending' CHECK (status IN (
        'pending', 'publishing', 'published', 'discarded')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT resource_lifecycle_outbox_operation_id_unique UNIQUE (operation_id)
);

CREATE INDEX idx_resource_lifecycle_outbox_lease
    ON resource_lifecycle_queue_outbox (status, created_at, updated_at)
    WHERE status IN ('pending', 'publishing');
