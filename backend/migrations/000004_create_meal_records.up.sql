CREATE TABLE meal_records (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint       NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Calendar date in Asia/Tokyo.
    date       date         NOT NULL,
    meal_type  text         NOT NULL CHECK (meal_type IN ('breakfast', 'lunch', 'dinner', 'snack')),
    content    text         NOT NULL CHECK (char_length(content) BETWEEN 1 AND 200),
    -- Totals for the portion eaten. NULL means unknown and is never treated as 0.
    calories   numeric(6,1) CHECK (calories  IS NULL OR calories  BETWEEN 0 AND 10000),
    protein_g  numeric(6,1) CHECK (protein_g IS NULL OR protein_g BETWEEN 0 AND 1000),
    fat_g      numeric(6,1) CHECK (fat_g     IS NULL OR fat_g     BETWEEN 0 AND 1000),
    carbs_g    numeric(6,1) CHECK (carbs_g   IS NULL OR carbs_g   BETWEEN 0 AND 1000),
    created_at timestamptz  NOT NULL DEFAULT now(),
    updated_at timestamptz  NOT NULL DEFAULT now()
);

CREATE INDEX meal_records_user_date_idx ON meal_records (user_id, date);
