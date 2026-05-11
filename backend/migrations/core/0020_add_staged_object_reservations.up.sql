CREATE TABLE IF NOT EXISTS staged_object_reservations (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL,
    object_key TEXT NOT NULL UNIQUE,
    kind VARCHAR(64) NOT NULL,
    bytes_reserved BIGINT NOT NULL CHECK (bytes_reserved >= 0),
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    released_at TIMESTAMP NULL
);

CREATE INDEX IF NOT EXISTS idx_staged_object_reservations_owner_status
    ON staged_object_reservations(owner_id, status);

CREATE INDEX IF NOT EXISTS idx_staged_object_reservations_status_updated
    ON staged_object_reservations(status, updated_at);
