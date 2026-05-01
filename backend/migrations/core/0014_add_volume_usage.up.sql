ALTER TABLE volumes
    ADD COLUMN used_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN usage_observed_at TIMESTAMP WITH TIME ZONE;

CREATE INDEX idx_volumes_owner_usage ON volumes (owner_id, used_bytes);
