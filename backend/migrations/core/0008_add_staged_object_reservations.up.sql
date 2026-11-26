-- Disk reserved for an uploaded build archive or Compose source while it is
-- staged in object storage. Released reservations are removed by the retention
-- worker.
CREATE TABLE staged_object_reservations (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL,
    object_key TEXT NOT NULL UNIQUE,
    kind VARCHAR(64) NOT NULL CHECK (kind IN ('build_archive', 'compose_source')),
    bytes_reserved BIGINT NOT NULL CHECK (bytes_reserved >= 0),
    status VARCHAR(32) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'released')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    released_at TIMESTAMPTZ
);

CREATE INDEX idx_staged_object_reservations_owner_status
    ON staged_object_reservations (owner_id, status);

CREATE INDEX idx_staged_object_reservations_status_updated
    ON staged_object_reservations (status, updated_at);
