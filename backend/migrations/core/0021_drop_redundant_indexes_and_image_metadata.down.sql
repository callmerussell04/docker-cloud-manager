ALTER TABLE images ADD COLUMN metadata JSONB;

CREATE INDEX idx_compose_deployment_jobs_stage
    ON compose_deployment_jobs (stage, updated_at)
    WHERE status IN ('running', 'canceling');

CREATE INDEX idx_resource_lifecycle_outbox_resource
    ON resource_lifecycle_queue_outbox (resource_type, resource_id, created_at DESC);

CREATE INDEX idx_images_status
    ON images (status);

CREATE INDEX idx_volumes_status
    ON volumes (status);

CREATE INDEX idx_volumes_owner_usage
    ON volumes (owner_id, used_bytes);

CREATE INDEX idx_containers_desired_status
    ON containers (desired_status);

CREATE INDEX idx_project_service_dependencies_project
    ON project_service_dependencies (project_id, container_id);

CREATE INDEX idx_project_services_project_order
    ON project_services (project_id, start_order);
