ALTER TABLE volumes
    DROP CONSTRAINT volumes_project_id_fkey,
    ADD CONSTRAINT volumes_project_id_fkey
        FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE;

DROP INDEX IF EXISTS idx_volumes_owner_name_unique;

ALTER TABLE volumes
    DROP COLUMN IF EXISTS name;
