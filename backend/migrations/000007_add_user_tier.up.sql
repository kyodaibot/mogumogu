-- The account tier (GLOSSARY: 账号等级), granted by an administrator with
-- cmd/admin. Existing users become normal.
ALTER TABLE users
    ADD COLUMN tier text NOT NULL DEFAULT 'normal'
    CONSTRAINT users_tier_check CHECK (tier IN ('normal', 'pro'));
