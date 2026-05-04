DROP INDEX IF EXISTS idx_builds_project_id_started_at;

ALTER TABLE builds
    DROP COLUMN project_service_name,
    DROP COLUMN project_id;
