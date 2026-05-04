ALTER TABLE builds
    ADD COLUMN project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
    ADD COLUMN project_service_name VARCHAR(100);

CREATE INDEX idx_builds_project_id_started_at
    ON builds (project_id, started_at DESC)
    WHERE project_id IS NOT NULL;
