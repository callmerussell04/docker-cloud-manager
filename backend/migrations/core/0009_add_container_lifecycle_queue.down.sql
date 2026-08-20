DROP INDEX IF EXISTS idx_compose_deployment_jobs_stage;

ALTER TABLE compose_deployment_jobs
    DROP COLUMN IF EXISTS resource_map_json,
    DROP COLUMN IF EXISTS plan_json,
    DROP COLUMN IF EXISTS stage;

DROP TABLE IF EXISTS container_lifecycle_queue_outbox;
