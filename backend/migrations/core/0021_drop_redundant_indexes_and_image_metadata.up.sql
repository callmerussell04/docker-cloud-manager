-- Indexes that repeat a constraint's own index.
DROP INDEX idx_project_services_project_order;          -- UNIQUE (project_id, start_order)
DROP INDEX idx_project_service_dependencies_project;    -- prefix of the primary key

-- Indexes no query can use: every read of these tables filters by owner, by
-- primary key, or matches most rows of the indexed column.
DROP INDEX idx_containers_desired_status;
DROP INDEX idx_volumes_owner_usage;
DROP INDEX idx_volumes_status;
DROP INDEX idx_images_status;
DROP INDEX idx_resource_lifecycle_outbox_resource;
DROP INDEX idx_compose_deployment_jobs_stage;

-- Never written with a value and never read.
ALTER TABLE images DROP COLUMN metadata;
