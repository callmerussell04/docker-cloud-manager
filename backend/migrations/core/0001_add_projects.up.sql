CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL,
    name VARCHAR(100) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN (
        'pending', 'building', 'deploying', 'canceling', 'canceled', 'starting',
        'running', 'stopping', 'stopped', 'failed', 'deleting')),
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX idx_projects_owner_name_unique
    ON projects (owner_id, name);

CREATE INDEX idx_projects_owner_created_at
    ON projects (owner_id, created_at DESC);
