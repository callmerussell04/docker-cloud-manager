DROP INDEX IF EXISTS idx_resource_operations_resource;
DROP INDEX IF EXISTS idx_resource_operations_active;
DROP TABLE IF EXISTS resource_operations;

ALTER TABLE images
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS last_observed_at,
    DROP COLUMN IF EXISTS status;

ALTER TABLE volumes
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS last_observed_at,
    DROP COLUMN IF EXISTS status;

ALTER TABLE containers
    DROP COLUMN IF EXISTS healthcheck,
    DROP COLUMN IF EXISTS restart_policy,
    DROP COLUMN IF EXISTS entrypoint,
    DROP COLUMN IF EXISTS command,
    DROP COLUMN IF EXISTS network_alias,
    DROP COLUMN IF EXISTS docker_generation,
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS last_observed_at,
    DROP COLUMN IF EXISTS desired_status;
