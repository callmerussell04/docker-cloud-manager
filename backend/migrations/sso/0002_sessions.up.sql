CREATE TABLE sessions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    idle_expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    revoked_reason TEXT CHECK (revoked_reason IN ('logout', 'replay', 'user_security_change'))
);
CREATE INDEX sessions_user_id ON sessions(user_id);
CREATE INDEX sessions_expiry ON sessions(absolute_expires_at);
CREATE TABLE refresh_tokens (
    digest BYTEA PRIMARY KEY CHECK (octet_length(digest) = 32),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    issued_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX refresh_tokens_current ON refresh_tokens(session_id) WHERE consumed_at IS NULL;
CREATE INDEX refresh_tokens_session ON refresh_tokens(session_id);
