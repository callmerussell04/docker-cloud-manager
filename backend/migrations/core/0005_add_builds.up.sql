CREATE TABLE builds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL,
    -- The user tag the build was queued for. The image reference is cleared
    -- when the image record goes (a failed or canceled build, a replaced or
    -- deleted image); the tag stays.
    tag TEXT NOT NULL,
    image_id UUID REFERENCES images(id) ON DELETE SET NULL,
    project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
    project_service_name VARCHAR(100),
    -- Where the build's context came from: an uploaded archive (its file
    -- name) or a Git repository (its URL and the ref asked for). A Compose
    -- project's build has none; its project and service name it.
    source_kind VARCHAR(16) NOT NULL DEFAULT '' CHECK (source_kind IN ('', 'archive', 'git')),
    source_location TEXT NOT NULL DEFAULT '',
    source_ref TEXT NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL CHECK (status IN (
        'pending', 'running', 'success', 'canceled', 'failed', 'failed_timeout',
        'failed_quota_exceeded', 'failed_resource_exhausted', 'failed_internal')),
    log_object_key TEXT NOT NULL,
    archive_object_key TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);

CREATE INDEX idx_builds_owner_id_created_at
    ON builds (owner_id, created_at DESC);

CREATE INDEX idx_builds_image_id_created_at
    ON builds (image_id, created_at DESC);

CREATE INDEX idx_builds_project_id_created_at
    ON builds (project_id, created_at DESC)
    WHERE project_id IS NOT NULL;

CREATE INDEX idx_builds_active_status
    ON builds (status, started_at)
    WHERE status IN ('pending', 'running');

CREATE INDEX idx_builds_owner_active
    ON builds (owner_id, status, started_at)
    WHERE status IN ('pending', 'running');

-- The payload holds the build arguments and is emptied once the row is
-- published or discarded.
CREATE TABLE build_queue_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
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
    CONSTRAINT build_queue_outbox_build_id_unique UNIQUE (build_id)
);

CREATE INDEX idx_build_queue_outbox_lease
    ON build_queue_outbox (status, created_at, updated_at)
    WHERE status IN ('pending', 'publishing');
