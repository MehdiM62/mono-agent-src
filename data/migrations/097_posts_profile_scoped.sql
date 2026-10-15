-- 097_posts_profile_scoped.sql
-- posts was UNIQUE(platform, shortcode) with no profile column, so two profiles
-- scraping the same post collided on one shared row. Give posts a profile_id
-- and scope the uniqueness to it (same rebuild as 014 did for people).
--
-- Backfill: profile_id comes from the owning person (people.profile_id via
-- person_id). Posts with a NULL or dangling person_id have no owner to infer
-- a profile from, so they go to the profile with id 'default' if it exists,
-- else to the oldest profile (created_at), and only if there are no profiles
-- at all to the literal 'default' (so the NOT NULL rebuild cannot fail).
--
-- Post ids are preserved, so post_comments.post_id keeps resolving. The
-- migration runner executes with foreign_keys=OFF (see ApplyMigrations), so
-- dropping the old posts does not cascade-delete post_comments; the
-- post_comments FK references posts by name and re-binds to the renamed table.

CREATE TABLE posts_v2 (
  id            TEXT PRIMARY KEY,
  person_id     TEXT REFERENCES people(id),
  platform      TEXT NOT NULL,
  shortcode     TEXT NOT NULL,
  url           TEXT NOT NULL,
  thumbnail_url TEXT,
  like_count    INTEGER,
  comment_count INTEGER,
  caption       TEXT,
  posted_at     TEXT,
  scraped_at    TEXT NOT NULL,
  profile_id    TEXT NOT NULL,
  UNIQUE(profile_id, platform, shortcode)
);

INSERT INTO posts_v2 (
  id, person_id, platform, shortcode, url, thumbnail_url, like_count,
  comment_count, caption, posted_at, scraped_at, profile_id
)
SELECT
  p.id, p.person_id, p.platform, p.shortcode, p.url, p.thumbnail_url,
  p.like_count, p.comment_count, p.caption, p.posted_at, p.scraped_at,
  COALESCE(
    (SELECT pe.profile_id FROM people pe WHERE pe.id = p.person_id),
    (SELECT id FROM profiles WHERE id = 'default'),
    (SELECT id FROM profiles ORDER BY created_at, id LIMIT 1),
    'default')
FROM posts p;

DROP TABLE posts;

ALTER TABLE posts_v2 RENAME TO posts;

CREATE INDEX IF NOT EXISTS idx_posts_profile ON posts(profile_id);
