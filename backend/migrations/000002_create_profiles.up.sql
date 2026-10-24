CREATE TABLE profiles (
    user_id            bigint       PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    height_cm          numeric(4,1) NOT NULL CHECK (height_cm BETWEEN 50 AND 300),
    weight_kg          numeric(4,1) NOT NULL CHECK (weight_kg BETWEEN 10 AND 500),
    age                integer      NOT NULL CHECK (age BETWEEN 10 AND 120),
    has_exercise_habit boolean      NOT NULL,
    -- NULL: not filled in yet. Empty array: no known allergens.
    allergens          text[],
    goal               text         NOT NULL CHECK (goal IN ('build_muscle', 'lose_fat')),
    created_at         timestamptz  NOT NULL DEFAULT now(),
    updated_at         timestamptz  NOT NULL DEFAULT now()
);
