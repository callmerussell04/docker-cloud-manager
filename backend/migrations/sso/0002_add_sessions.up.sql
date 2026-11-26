CREATE TABLE sessions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    idle_expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    revoked_reason TEXT CHECK (revoked_reason IN ('logout', 'replay', 'user_security_change', 'provider_logout')),
    -- Set only for a session created by an OIDC callback: the provider, its
    -- session ID (the ID token's sid) and the ID token, kept as the hint of
    -- the provider sign-out URL until the session is revoked.
    provider TEXT,
    provider_session_id TEXT,
    id_token TEXT,
    CONSTRAINT sessions_revocation_complete
        CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL)),
    CONSTRAINT sessions_provider_fields
        CHECK (provider IS NOT NULL OR (provider_session_id IS NULL AND id_token IS NULL)),
    CONSTRAINT sessions_revoked_without_id_token
        CHECK (revoked_at IS NULL OR id_token IS NULL)
);

CREATE INDEX sessions_user_id ON sessions (user_id);
CREATE INDEX sessions_expiry ON sessions (absolute_expires_at);
-- A back-channel logout names the provider session whose DCM sessions end.
CREATE INDEX sessions_provider_session ON sessions (provider, provider_session_id)
    WHERE provider_session_id IS NOT NULL;

-- A session has one current refresh token; consumed ones stay for replay
-- detection until the session is deleted.
CREATE TABLE refresh_tokens (
    digest BYTEA PRIMARY KEY CHECK (octet_length(digest) = 32),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    issued_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX refresh_tokens_current ON refresh_tokens (session_id) WHERE consumed_at IS NULL;
CREATE INDEX refresh_tokens_session ON refresh_tokens (session_id);
