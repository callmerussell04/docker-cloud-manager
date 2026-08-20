CREATE TABLE images (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL,
    tag VARCHAR(100) NOT NULL,
    size_mb INTEGER NOT NULL CHECK (size_mb >= 0),
    metadata JSONB,
    status VARCHAR(20) NOT NULL DEFAULT 'available',
    last_observed_at TIMESTAMP WITH TIME ZONE,
    last_error TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX idx_images_owner_id_tag
    ON images (owner_id, tag);

CREATE INDEX idx_images_owner_created_at
    ON images (owner_id, created_at DESC);

CREATE INDEX idx_images_status
    ON images (status);

