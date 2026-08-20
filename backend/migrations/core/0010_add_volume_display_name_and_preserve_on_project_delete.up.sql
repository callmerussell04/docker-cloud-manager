ALTER TABLE volumes
    ADD COLUMN name VARCHAR(100);

UPDATE volumes
SET name = CASE
    WHEN docker_name LIKE ('vol_' || substring(owner_id::text from 1 for 8) || '\_%') ESCAPE '\'
        THEN substring(docker_name from length('vol_' || substring(owner_id::text from 1 for 8) || '_') + 1)
    ELSE docker_name
END;

ALTER TABLE volumes
    ALTER COLUMN name SET NOT NULL;

CREATE UNIQUE INDEX idx_volumes_owner_name_unique
    ON volumes (owner_id, name);

ALTER TABLE volumes
    DROP CONSTRAINT volumes_project_id_fkey,
    ADD CONSTRAINT volumes_project_id_fkey
        FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL;
