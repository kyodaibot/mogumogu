ALTER TABLE users ADD COLUMN campus text NOT NULL DEFAULT 'yoshida'
    CHECK (campus IN ('yoshida', 'uji', 'katsura'));
ALTER TABLE users ALTER COLUMN campus DROP DEFAULT;
