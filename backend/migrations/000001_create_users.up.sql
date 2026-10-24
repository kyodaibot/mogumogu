CREATE TABLE users (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Stored lowercase; the API compares usernames case-insensitively.
    username      text        NOT NULL,
    password_hash text        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_username_key UNIQUE (username),
    CONSTRAINT users_username_format CHECK (username ~ '^[a-z0-9_]{3,32}$')
);
