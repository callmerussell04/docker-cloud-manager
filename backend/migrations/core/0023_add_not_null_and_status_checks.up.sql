-- Columns the code always writes and always reads as present.
ALTER TABLE projects
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN status SET NOT NULL;

ALTER TABLE containers
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN domain_prefix SET NOT NULL,
    ALTER COLUMN network_alias SET NOT NULL,
    ALTER COLUMN restart_policy SET NOT NULL;

ALTER TABLE volumes ALTER COLUMN created_at SET NOT NULL;
ALTER TABLE volume_mounts ALTER COLUMN is_readonly SET NOT NULL;
ALTER TABLE images ALTER COLUMN created_at SET NOT NULL;
ALTER TABLE project_volumes ALTER COLUMN created_at SET NOT NULL;

ALTER TABLE build_queue_outbox
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET NOT NULL;
ALTER TABLE compose_deployment_queue_outbox
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET NOT NULL;
ALTER TABLE container_lifecycle_queue_outbox
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET NOT NULL;
ALTER TABLE resource_lifecycle_queue_outbox
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET NOT NULL;
ALTER TABLE compose_deployment_jobs
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET NOT NULL;
ALTER TABLE resource_operations
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET NOT NULL;
ALTER TABLE staged_object_reservations
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET NOT NULL;

-- Status-like columns accept only the values the model defines. Partial
-- indexes and queries name these values literally, so a misspelt one must not
-- be storable.
ALTER TABLE projects
    ADD CONSTRAINT projects_status_check CHECK (status IN (
        'pending', 'building', 'deploying', 'canceling', 'canceled', 'starting',
        'running', 'stopping', 'stopped', 'failed', 'deleting'));

ALTER TABLE containers
    ADD CONSTRAINT containers_status_check CHECK (status IN (
        'pending', 'creating', 'created', 'starting', 'running', 'stopping',
        'exited', 'exposing', 'error', 'missing', 'deleting', 'reconciling')),
    ADD CONSTRAINT containers_desired_status_check CHECK (desired_status IN (
        'pending', 'creating', 'created', 'starting', 'running', 'stopping',
        'exited', 'exposing', 'error', 'missing', 'deleting', 'reconciling')),
    ADD CONSTRAINT containers_internal_port_check CHECK (internal_port BETWEEN 0 AND 65535);

ALTER TABLE volumes
    ADD CONSTRAINT volumes_status_check CHECK (status IN (
        'creating', 'available', 'deleting', 'missing', 'error'));

ALTER TABLE images
    ADD CONSTRAINT images_status_check CHECK (status IN (
        'building', 'available', 'deleting', 'missing', 'error'));

ALTER TABLE builds
    ADD CONSTRAINT builds_status_check CHECK (status IN (
        'pending', 'running', 'success', 'canceled', 'failed', 'failed_timeout',
        'failed_quota_exceeded', 'failed_resource_exhausted', 'failed_internal'));

ALTER TABLE compose_deployment_jobs
    ADD CONSTRAINT compose_deployment_jobs_status_check CHECK (status IN (
        'queued', 'running', 'canceling', 'canceled', 'succeeded', 'failed')),
    ADD CONSTRAINT compose_deployment_jobs_stage_check CHECK (stage IN (
        '', 'planning', 'building', 'creating', 'starting', 'rolling_back'));

ALTER TABLE resource_operations
    ADD CONSTRAINT resource_operations_resource_type_check CHECK (resource_type IN (
        'container', 'volume', 'image', 'project', 'build')),
    ADD CONSTRAINT resource_operations_operation_check CHECK (operation IN (
        'create', 'start', 'stop', 'delete', 'expose')),
    ADD CONSTRAINT resource_operations_status_check CHECK (status IN (
        'pending', 'running', 'done', 'failed'));

ALTER TABLE staged_object_reservations
    ADD CONSTRAINT staged_object_reservations_kind_check CHECK (kind IN (
        'build_archive', 'compose_source')),
    ADD CONSTRAINT staged_object_reservations_status_check CHECK (status IN (
        'active', 'released'));

ALTER TABLE build_queue_outbox
    ADD CONSTRAINT build_queue_outbox_status_check CHECK (status IN (
        'pending', 'publishing', 'published', 'discarded'));
ALTER TABLE compose_deployment_queue_outbox
    ADD CONSTRAINT compose_deployment_queue_outbox_status_check CHECK (status IN (
        'pending', 'publishing', 'published', 'discarded'));
ALTER TABLE container_lifecycle_queue_outbox
    ADD CONSTRAINT container_lifecycle_queue_outbox_status_check CHECK (status IN (
        'pending', 'publishing', 'published', 'discarded'));
ALTER TABLE resource_lifecycle_queue_outbox
    ADD CONSTRAINT resource_lifecycle_queue_outbox_status_check CHECK (status IN (
        'pending', 'publishing', 'published', 'discarded'));
