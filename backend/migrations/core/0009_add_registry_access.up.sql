-- The credential a claimed build pushes with. No foreign key to builds: the
-- row is the authority for Registry cleanup after the build row is gone, until
-- the retention worker removes it.
CREATE TABLE registry_build_credentials (
    build_id UUID PRIMARY KEY,
    owner_id UUID NOT NULL,
    repository TEXT NOT NULL,
    tag TEXT NOT NULL,
    secret_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(secret_hash) = 32),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    last_issued_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX registry_credentials_repository_idx
    ON registry_build_credentials (repository);

CREATE TABLE registry_pull_credentials (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL,
    repository TEXT NOT NULL,
    secret_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(secret_hash) = 32),
    expires_at TIMESTAMPTZ NOT NULL
);

-- No cascading foreign key: cleanup authority must survive removal of build
-- and image rows.
CREATE TABLE registry_cleanup_jobs (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL,
    repository TEXT NOT NULL,
    tag TEXT NOT NULL,
    not_before TIMESTAMPTZ NOT NULL
);

-- One row gates Registry garbage collection against build claims. A live lease
-- (lease_until in the future) or a set draining_since refuses every claim; the
-- GC worker takes the lease only while no build runs. last_completed_at starts
-- at installation, so a fresh platform drains no earlier than one day later.
CREATE TABLE registry_gc_state (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    lease_until TIMESTAMPTZ,
    draining_since TIMESTAMPTZ,
    last_completed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO registry_gc_state (id) VALUES (TRUE);
