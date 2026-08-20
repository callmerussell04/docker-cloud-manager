CREATE TABLE containers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL,
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    docker_id VARCHAR(64) UNIQUE,
    name VARCHAR(100) NOT NULL,
    image_tag VARCHAR(150) NOT NULL,
    internal_port INTEGER,
    domain_prefix VARCHAR(255) DEFAULT '',
    status VARCHAR(20) NOT NULL,
    desired_status VARCHAR(20) NOT NULL DEFAULT 'created',
    ttl_deadline TIMESTAMP WITH TIME ZONE,
    env_vars JSONB,
    base_memory_reservation BIGINT NOT NULL CHECK (base_memory_reservation >= 0),
    last_observed_at TIMESTAMP WITH TIME ZONE,
    last_error TEXT,
    last_exit_code INTEGER,
    docker_generation INTEGER NOT NULL DEFAULT 1 CHECK (docker_generation > 0),
    network_alias VARCHAR(100) DEFAULT '',
    command JSONB,
    entrypoint JSONB,
    restart_policy VARCHAR(32) DEFAULT '',
    healthcheck JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX idx_containers_domain_prefix
    ON containers (domain_prefix)
    WHERE domain_prefix != '';

CREATE INDEX idx_containers_owner_id_image_tag
    ON containers (owner_id, image_tag);

CREATE INDEX idx_containers_owner_created_at
    ON containers (owner_id, created_at DESC);

CREATE INDEX idx_containers_project_id
    ON containers (project_id)
    WHERE project_id IS NOT NULL;

CREATE INDEX idx_containers_status
    ON containers (status);

CREATE INDEX idx_containers_desired_status
    ON containers (desired_status);

