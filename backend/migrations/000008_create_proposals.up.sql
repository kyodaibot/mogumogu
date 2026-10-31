-- Change proposals (GLOSSARY: 变更提案): changes to meal records the meal
-- assistant staged, run only after the user confirms them. Rows are kept after
-- they run or expire; they are the audit record of who proposed and ran what.
CREATE TABLE proposals (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Each change with the record as it was when proposed (the optimistic lock).
    changes     jsonb       NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- Both NULL until the proposal runs; results has one entry per change.
    executed_at timestamptz,
    results     jsonb,
    CHECK ((executed_at IS NULL) = (results IS NULL))
);

CREATE INDEX proposals_user_idx ON proposals (user_id);
