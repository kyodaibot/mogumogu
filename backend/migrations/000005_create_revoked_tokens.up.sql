CREATE TABLE revoked_tokens (
    jti        text        PRIMARY KEY,
    -- Rows can be deleted once the token would have expired anyway.
    expires_at timestamptz NOT NULL
);

CREATE INDEX revoked_tokens_expires_at_idx ON revoked_tokens (expires_at);
