-- Profile info: the personal details a profile holds for filling web forms
-- (name, address, IDs, work, ...), one row per (profile, field).
--
--   key        a field key from internal/profileinfo's catalog
--   value      the text; for fields the catalog marks sensitive (ID numbers,
--              bank details) and for card fields it is a vaultenc: blob, the
--              same encryption the vault uses. For photo fields it is a path
--              relative to the profile's folder.
--   source     'user' (typed by the person) or 'ai' (extracted from the
--              profile's documents); the AI never overwrites a 'user' row
--   origin     for an 'ai' row, the document it came from
CREATE TABLE IF NOT EXISTS profile_info (
    profile_id TEXT NOT NULL,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    source     TEXT NOT NULL DEFAULT 'user',
    origin     TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL,
    PRIMARY KEY (profile_id, key)
);
