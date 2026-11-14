ALTER TABLE volumes
    ADD COLUMN project_id UUID REFERENCES projects(id) ON DELETE SET NULL;

UPDATE volumes v
SET project_id = (
    SELECT pv.project_id
    FROM project_volumes pv
    WHERE pv.volume_id = v.id
    ORDER BY pv.created_at, pv.project_id
    LIMIT 1
);

CREATE INDEX idx_volumes_project_id
    ON volumes (project_id)
    WHERE project_id IS NOT NULL;
