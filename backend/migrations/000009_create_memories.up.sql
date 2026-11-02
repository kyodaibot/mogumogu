-- Memories (GLOSSARY: 记忆): lasting preferences or facts the user stated, which
-- the meal assistant reads in every conversation (Pro). Saved without a
-- confirmation card; the user sees each change and can delete it.
CREATE TABLE memories (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    content    text        NOT NULL CHECK (char_length(content) BETWEEN 1 AND 100),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- The same memory twice is refused; the index also serves lookups by user.
CREATE UNIQUE INDEX memories_user_content_idx ON memories (user_id, content);
