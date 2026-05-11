CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE user_role AS ENUM ('admin', 'user');
CREATE TYPE user_status AS ENUM ('active', 'deactivated');
CREATE TYPE auth_source AS ENUM ('local', 'oidc');

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(30) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255),
    role user_role NOT NULL DEFAULT 'user',
    status user_status NOT NULL DEFAULT 'active',
    quota_cpu DECIMAL(3,1) DEFAULT 1.0,
    quota_ram_mb INTEGER DEFAULT 2048,
    quota_disk_mb INTEGER DEFAULT 5120,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    auth_source auth_source NOT NULL DEFAULT 'local',
    external_provider VARCHAR(64),
    external_subject VARCHAR(255),
    external_username VARCHAR(255),
    last_login_at TIMESTAMP WITH TIME ZONE
);

CREATE UNIQUE INDEX idx_users_external_identity
    ON users (external_provider, external_subject)
    WHERE external_provider IS NOT NULL AND external_subject IS NOT NULL;

CREATE INDEX idx_users_status
    ON users (status);

CREATE INDEX idx_users_auth_source
    ON users (auth_source);

CREATE INDEX idx_users_created_at
    ON users (created_at DESC, id DESC);

