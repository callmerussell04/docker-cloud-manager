CREATE TABLE volumes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL,
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    docker_name VARCHAR(100) UNIQUE NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'available',
    last_observed_at TIMESTAMP WITH TIME ZONE,
    last_error TEXT,
    used_bytes BIGINT NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
    usage_observed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE volume_mounts (
    container_id UUID NOT NULL REFERENCES containers(id) ON DELETE CASCADE,
    volume_id UUID NOT NULL REFERENCES volumes(id) ON DELETE CASCADE,
    mount_path VARCHAR(255) NOT NULL,
    is_readonly BOOLEAN DEFAULT false,
    PRIMARY KEY (container_id, volume_id)
);

CREATE INDEX idx_volumes_owner_created_at
    ON volumes (owner_id, created_at DESC);

CREATE INDEX idx_volumes_project_id
    ON volumes (project_id)
    WHERE project_id IS NOT NULL;

CREATE INDEX idx_volumes_owner_usage
    ON volumes (owner_id, used_bytes);

CREATE INDEX idx_volumes_status
    ON volumes (status);

CREATE INDEX idx_volume_mounts_volume_id
    ON volume_mounts (volume_id);

