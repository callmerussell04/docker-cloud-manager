-- An OIDC sign-in between start and callback. Only digests of the state and
-- of the browser-binding secret are stored; the nonce and PKCE verifier are
-- kept as they are because the code exchange needs them.
CREATE TABLE oidc_login_attempts (
    state_digest BYTEA PRIMARY KEY CHECK (octet_length(state_digest) = 32),
    provider TEXT NOT NULL,
    nonce TEXT NOT NULL,
    pkce_verifier TEXT NOT NULL,
    binding_digest BYTEA NOT NULL CHECK (octet_length(binding_digest) = 32),
    redirect_after TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX oidc_login_attempts_expiry ON oidc_login_attempts (expires_at);
