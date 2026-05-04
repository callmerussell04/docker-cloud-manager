CREATE TABLE volumes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL,
    docker_name VARCHAR(100) UNIQUE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE volume_mounts (
    container_id UUID REFERENCES containers(id) ON DELETE CASCADE,
    volume_id UUID REFERENCES volumes(id) ON DELETE CASCADE,
    mount_path VARCHAR(255) NOT NULL,
    is_readonly BOOLEAN DEFAULT false,
    PRIMARY KEY (container_id, volume_id)
);
