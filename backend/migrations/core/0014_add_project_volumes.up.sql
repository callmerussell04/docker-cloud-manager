CREATE TABLE project_volumes (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    volume_id UUID NOT NULL REFERENCES volumes(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (project_id, volume_id)
);

CREATE INDEX idx_project_volumes_volume_id
    ON project_volumes (volume_id);

INSERT INTO project_volumes (project_id, volume_id)
SELECT project_id, id
FROM volumes
WHERE project_id IS NOT NULL
ON CONFLICT DO NOTHING;
