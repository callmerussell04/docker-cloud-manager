CREATE TABLE compose_deployment_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    owner_id UUID NOT NULL,
    source_type VARCHAR(32) NOT NULL CHECK (source_type IN ('upload', 'git')),
    source_object_key TEXT NOT NULL,
    compose_file TEXT NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'queued',
    attempts INTEGER NOT NULL DEFAULT 0,
    cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
    error_message TEXT,
    request_id TEXT,
    started_at TIMESTAMP WITH TIME ZONE,
    finished_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_compose_deployment_jobs_project
    ON compose_deployment_jobs (project_id);

CREATE INDEX idx_compose_deployment_jobs_active
    ON compose_deployment_jobs (status, created_at)
    WHERE status IN ('queued', 'running', 'canceling');

CREATE TABLE compose_deployment_queue_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES compose_deployment_jobs(id) ON DELETE CASCADE,
    exchange VARCHAR(128) NOT NULL,
    routing_key VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    published_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT compose_deployment_outbox_job_id_unique UNIQUE (job_id)
);

CREATE INDEX idx_compose_deployment_outbox_pending
    ON compose_deployment_queue_outbox (created_at)
    WHERE status = 'pending';
