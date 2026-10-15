-- 100_tags_profile_unique.sql
-- tags.name was UNIQUE across the whole machine (migration 004), so two
-- profiles could not both own a tag called "lead" even though every tag row
-- belongs to one profile (migration 011). Widen the uniqueness to
-- (profile_id, name), still case-insensitive. SQLite cannot alter a UNIQUE
-- constraint, so rebuild the table the way migration 014 did for people.
-- Tag ids are copied unchanged, so people_tags (tag_id REFERENCES tags(id))
-- keeps pointing at the same tags; foreign keys are off while migrations run.

CREATE TABLE tags_v2 (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL COLLATE NOCASE,
    color      TEXT NOT NULL DEFAULT '#00b4d8',
    profile_id TEXT NOT NULL DEFAULT 'default',
    UNIQUE(profile_id, name)
);

INSERT INTO tags_v2 (id, name, color, profile_id)
SELECT id, name, color, profile_id FROM tags;

DROP TABLE tags;

ALTER TABLE tags_v2 RENAME TO tags;

CREATE INDEX IF NOT EXISTS idx_tags_profile ON tags(profile_id);
