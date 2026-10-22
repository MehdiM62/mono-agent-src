-- data/migrations/115_application_assets.sql
-- Everything around one application in one place (epic #951, issue #954):
--   * the statuses awaiting_submit and needs_you join the lifecycle. The
--     status column carries a CHECK, so the applications table is rebuilt
--     (create / copy / drop / rename; migrations run with foreign keys off,
--     so the children keep their rows).
--   * application_links ties an application to a person, message, task,
--     document or capture by id.
--   * application_fills holds the last form fill (status, screenshot, note)
--     and application_fill_fields the fields it wrote.
CREATE TABLE applications_v2 (
    id          TEXT PRIMARY KEY,
    profile_id  TEXT NOT NULL DEFAULT 'default',
    kind        TEXT NOT NULL CHECK (kind IN ('job', 'tender')),
    status      TEXT NOT NULL CHECK (status IN ('pending', 'awaiting_submit', 'needs_you', 'applied', 'rejected', 'cancelled')),
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
INSERT INTO applications_v2 (id, profile_id, kind, status, created_at, updated_at)
    SELECT id, profile_id, kind, status, created_at, updated_at FROM applications;
DROP TABLE applications;
ALTER TABLE applications_v2 RENAME TO applications;
CREATE INDEX IF NOT EXISTS idx_applications_profile ON applications(profile_id);
CREATE INDEX IF NOT EXISTS idx_applications_kind    ON applications(kind);
CREATE INDEX IF NOT EXISTS idx_applications_status  ON applications(status);

CREATE TABLE IF NOT EXISTS application_links (
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    kind           TEXT NOT NULL CHECK (kind IN ('person', 'message', 'task', 'document', 'capture')),
    ref_id         TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    PRIMARY KEY (application_id, kind, ref_id)
);

CREATE TABLE IF NOT EXISTS application_fills (
    application_id  TEXT PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    status          TEXT NOT NULL CHECK (status IN ('filling', 'filled', 'needs_you', 'submitted')),
    screenshot_path TEXT NOT NULL DEFAULT '',
    note            TEXT NOT NULL DEFAULT '',
    updated_at      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS application_fill_fields (
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    label          TEXT NOT NULL,
    value          TEXT NOT NULL DEFAULT '',
    source         TEXT NOT NULL DEFAULT '',
    generated      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_app_fill_fields_app ON application_fill_fields(application_id);
