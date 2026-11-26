CREATE TABLE project_services (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    container_id UUID NOT NULL REFERENCES containers(id) ON DELETE CASCADE,
    service_name VARCHAR(100) NOT NULL,
    start_order INTEGER NOT NULL,
    PRIMARY KEY (project_id, container_id),
    UNIQUE (project_id, service_name),
    UNIQUE (project_id, start_order)
);

CREATE TABLE project_service_dependencies (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    container_id UUID NOT NULL REFERENCES containers(id) ON DELETE CASCADE,
    depends_on_container_id UUID NOT NULL REFERENCES containers(id) ON DELETE CASCADE,
    condition VARCHAR(64) NOT NULL CHECK (condition IN (
        'service_started', 'service_healthy', 'service_completed_successfully')),
    required BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (project_id, container_id, depends_on_container_id)
);

-- plan_json holds environment values and build arguments; it is removed when
-- the job reaches a terminal status. resource_map_json holds only IDs.
CREATE TABLE compose_deployment_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    owner_id UUID NOT NULL,
    source_type VARCHAR(32) NOT NULL CHECK (source_type IN ('upload', 'git')),
    source_object_key TEXT NOT NULL,
    compose_file TEXT NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'queued' CHECK (status IN (
        'queued', 'running', 'canceling', 'canceled', 'succeeded', 'failed')),
    stage VARCHAR(32) NOT NULL DEFAULT '' CHECK (stage IN (
        '', 'planning', 'building', 'creating', 'starting', 'rolling_back')),
    plan_json JSONB,
    resource_map_json JSONB,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
    error_message TEXT,
    request_id TEXT,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_compose_deployment_jobs_project
    ON compose_deployment_jobs (project_id);

CREATE INDEX idx_compose_deployment_jobs_active
    ON compose_deployment_jobs (status, created_at)
    WHERE status IN ('queued', 'running', 'canceling');

CREATE INDEX idx_compose_deployment_jobs_owner_active
    ON compose_deployment_jobs (owner_id, status, created_at)
    WHERE status IN ('queued', 'running', 'canceling');

CREATE INDEX idx_compose_deployment_jobs_source_object_key
    ON compose_deployment_jobs (source_object_key);

CREATE TABLE compose_deployment_queue_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES compose_deployment_jobs(id) ON DELETE CASCADE,
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
    CONSTRAINT compose_deployment_outbox_job_id_unique UNIQUE (job_id)
);

CREATE INDEX idx_compose_deployment_outbox_lease
    ON compose_deployment_queue_outbox (status, created_at, updated_at)
    WHERE status IN ('pending', 'publishing');
