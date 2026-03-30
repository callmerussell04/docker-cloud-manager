CREATE TABLE containers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL REFERENCES users(id),
    docker_id VARCHAR(64) UNIQUE,
    name VARCHAR(100) NOT NULL,
    image_tag VARCHAR(150) NOT NULL,
    internal_port INTEGER,
    status VARCHAR(20) NOT NULL,
    ttl_deadline TIMESTAMP WITH TIME ZONE,
    env_vars JSONB,
    base_memory_reservation BIGINT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);