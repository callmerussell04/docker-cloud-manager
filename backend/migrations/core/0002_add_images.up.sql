CREATE TABLE images (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL,
    tag VARCHAR(100) NOT NULL,
    size_mb INTEGER NOT NULL,
    is_custom BOOLEAN DEFAULT false,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
