-- A user's custom quota. Core owns quotas so that admission never depends on
-- the identity service; an owner with no row has the runtime default quota.
-- CPU is stored in millicores so admission's millicore arithmetic stays exact.
CREATE TABLE user_quotas (
    owner_id UUID PRIMARY KEY,
    cpu_millicores BIGINT NOT NULL CHECK (cpu_millicores > 0),
    ram_mb BIGINT NOT NULL CHECK (ram_mb > 0),
    disk_mb BIGINT NOT NULL CHECK (disk_mb > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
