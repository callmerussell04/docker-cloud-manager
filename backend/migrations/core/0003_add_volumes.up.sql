CREATE TABLE volumes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL,
    -- The user-facing name; docker_name is the volume's name on the host.
    name VARCHAR(100) NOT NULL,
    docker_name VARCHAR(100) NOT NULL UNIQUE,
    status VARCHAR(20) NOT NULL DEFAULT 'available' CHECK (status IN (
        'creating', 'available', 'deleting', 'missing', 'error')),
    -- The volume size is reserved against its owner's disk quota from
    -- creation and never changes; size_enforced records whether the kernel
    -- enforces it.
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 1048576),
    size_enforced BOOLEAN NOT NULL DEFAULT FALSE,
    used_bytes BIGINT NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
    usage_observed_at TIMESTAMPTZ,
    last_observed_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX idx_volumes_owner_name_unique
    ON volumes (owner_id, name);

CREATE INDEX idx_volumes_owner_created_at
    ON volumes (owner_id, created_at DESC);

-- One container path holds one volume, and one volume is mounted once per
-- container.
CREATE TABLE volume_mounts (
    container_id UUID NOT NULL REFERENCES containers(id) ON DELETE CASCADE,
    volume_id UUID NOT NULL REFERENCES volumes(id) ON DELETE CASCADE,
    mount_path VARCHAR(255) NOT NULL,
    is_readonly BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (container_id, volume_id),
    CONSTRAINT volume_mounts_container_mount_path_unique UNIQUE (container_id, mount_path)
);

CREATE INDEX idx_volume_mounts_volume_id
    ON volume_mounts (volume_id);

-- The only record of which projects a volume belongs to. Deleting a project
-- removes its links and keeps the volumes.
CREATE TABLE project_volumes (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    volume_id UUID NOT NULL REFERENCES volumes(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (project_id, volume_id)
);

CREATE INDEX idx_project_volumes_volume_id
    ON project_volumes (volume_id);
