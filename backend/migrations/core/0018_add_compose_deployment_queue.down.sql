DROP INDEX IF EXISTS idx_compose_deployment_outbox_pending;
DROP TABLE IF EXISTS compose_deployment_queue_outbox;
DROP INDEX IF EXISTS idx_compose_deployment_jobs_active;
DROP INDEX IF EXISTS idx_compose_deployment_jobs_project;
DROP TABLE IF EXISTS compose_deployment_jobs;
