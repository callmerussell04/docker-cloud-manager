CREATE TABLE containers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL,
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    image_tag VARCHAR(150) NOT NULL,
    -- The Docker runtime of the recorded generation; NULL until it is created.
    docker_id VARCHAR(64) UNIQUE,
    docker_generation INTEGER NOT NULL DEFAULT 1 CHECK (docker_generation > 0),
    status VARCHAR(20) NOT NULL CHECK (status IN (
        'pending', 'creating', 'created', 'starting', 'running', 'stopping',
        'exited', 'exposing', 'error', 'missing', 'deleting', 'reconciling')),
    desired_status VARCHAR(20) NOT NULL DEFAULT 'created' CHECK (desired_status IN (
        'pending', 'creating', 'created', 'starting', 'running', 'stopping',
        'exited', 'exposing', 'error', 'missing', 'deleting', 'reconciling')),
    -- Reservations: memory in bytes, CPU in millicores.
    base_memory_reservation BIGINT NOT NULL CHECK (base_memory_reservation >= 0),
    base_cpu_millicores BIGINT NOT NULL DEFAULT 250 CHECK (base_cpu_millicores >= 0),
    -- Exposure: an empty prefix means the container is not exposed.
    domain_prefix VARCHAR(255) NOT NULL DEFAULT '',
    internal_port INTEGER CHECK (internal_port BETWEEN 0 AND 65535),
    network_alias VARCHAR(100) NOT NULL DEFAULT '',
    env_vars JSONB,
    command JSONB,
    entrypoint JSONB,
    restart_policy VARCHAR(32) NOT NULL DEFAULT '',
    healthcheck JSONB,
    ttl_deadline TIMESTAMPTZ,
    last_observed_at TIMESTAMPTZ,
    last_error TEXT,
    last_exit_code INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX idx_containers_owner_name_unique
    ON containers (owner_id, name);

CREATE UNIQUE INDEX idx_containers_domain_prefix
    ON containers (domain_prefix)
    WHERE domain_prefix != '';

CREATE INDEX idx_containers_owner_created_at
    ON containers (owner_id, created_at DESC);

CREATE INDEX idx_containers_owner_id_image_tag
    ON containers (owner_id, image_tag);

CREATE INDEX idx_containers_project_id
    ON containers (project_id)
    WHERE project_id IS NOT NULL;

CREATE INDEX idx_containers_status
    ON containers (status);
