CREATE TABLE builds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    image_id UUID NOT NULL,
    owner_id UUID NOT NULL,
    project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
    project_service_name VARCHAR(100),
    status VARCHAR(32) NOT NULL,
    log_file_path TEXT NOT NULL,
    archive_object_key TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_builds_owner_id_started_at
    ON builds (owner_id, started_at DESC);

CREATE INDEX idx_builds_image_id_started_at
    ON builds (image_id, started_at DESC);

CREATE INDEX idx_builds_project_id_started_at
    ON builds (project_id, started_at DESC)
    WHERE project_id IS NOT NULL;

CREATE INDEX idx_builds_active_status
    ON builds (status, started_at)
    WHERE status IN ('pending', 'running');

CREATE INDEX idx_builds_owner_active
    ON builds (owner_id, status, started_at)
    WHERE status IN ('pending', 'running');

CREATE TABLE build_queue_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    exchange VARCHAR(128) NOT NULL,
    routing_key VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT,
    published_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT build_queue_outbox_build_id_unique UNIQUE (build_id)
);

CREATE INDEX idx_build_queue_outbox_lease
    ON build_queue_outbox (status, created_at, updated_at)
    WHERE status IN ('pending', 'publishing');

