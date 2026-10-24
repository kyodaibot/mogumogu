CREATE TABLE schedule_events (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title      text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 100),
    starts_at  timestamptz NOT NULL,
    ends_at    timestamptz NOT NULL,
    notes      text        CHECK (notes IS NULL OR char_length(notes) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- May cross midnight, but at most 24 hours long.
    CONSTRAINT schedule_events_time_range
        CHECK (ends_at > starts_at AND ends_at - starts_at <= interval '24 hours')
);

CREATE INDEX schedule_events_user_starts_at_idx ON schedule_events (user_id, starts_at);
