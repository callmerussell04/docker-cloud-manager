CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL,
    name VARCHAR(100) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    status VARCHAR(20) DEFAULT 'pending',
    error_message TEXT
);

CREATE INDEX idx_projects_owner_created_at
    ON projects (owner_id, created_at DESC);

