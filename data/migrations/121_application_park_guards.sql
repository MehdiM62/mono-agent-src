-- data/migrations/121_application_park_guards.sql
-- Two guards for parked application tabs (epic #951, issue #956):
--   * application_tab_opens records which apply task opened which tab, so a
--     fill result can only claim a tab its own task opened.
--   * application_slots is the profile-wide reservation behind
--     `application fill queue --max-parked`: one row per fill in flight, owned
--     by a queue process and leased, so concurrent queues share one cap and a
--     dead queue's rows expire.
CREATE TABLE IF NOT EXISTS application_tab_opens (
    task_id   TEXT NOT NULL,
    tab_id    INTEGER NOT NULL CHECK (tab_id > 0),
    opened_at TEXT NOT NULL,
    PRIMARY KEY (task_id, tab_id)
);
CREATE TABLE IF NOT EXISTS application_slots (
    application_id TEXT PRIMARY KEY,
    owner          TEXT NOT NULL,
    pid            INTEGER NOT NULL,
    lease_until    INTEGER NOT NULL,
    created_at     TEXT NOT NULL
);
