-- Messages sent to the meal assistant per user and Tokyo calendar day (the
-- usage quota). The site-wide count is the sum over a day's rows.
CREATE TABLE assistant_usage (
    day      date    NOT NULL,
    user_id  bigint  NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    messages integer NOT NULL CHECK (messages >= 0),
    PRIMARY KEY (day, user_id)
);
