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
CREATE INDEX registry_credentials_repository_idx ON registry_build_credentials(repository);

-- No cascading FK: cleanup authority must survive removal of build/image rows.
CREATE TABLE registry_cleanup_jobs (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL,
    repository TEXT NOT NULL,
    tag TEXT NOT NULL,
    not_before TIMESTAMPTZ NOT NULL
);

CREATE TABLE registry_pull_credentials (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL,
    repository TEXT NOT NULL,
    secret_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(secret_hash) = 32),
    expires_at TIMESTAMPTZ NOT NULL
);
