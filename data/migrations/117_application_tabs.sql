-- data/migrations/117_application_tabs.sql
-- Parked application tabs (epic #951, issue #956): the agent fills an
-- application, the tab stays open at awaiting_submit and the person submits.
--   * application_tabs binds one application to the browser tab its fill left
--     open. parked: the agent may not touch it; active: one task (task_id) is
--     working in it; closed: the application is over; lost: the tab is gone.
--     A live tab belongs to one application at most.
--   * application_fills.submit_observed_at: the extension saw a trusted submit
--     in the parked tab. It is a hint for the person to confirm; it never
--     changes the application's status (applied stays the person's).
CREATE TABLE IF NOT EXISTS application_tabs (
    application_id TEXT PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    task_id        TEXT NOT NULL,
    tab_id         INTEGER NOT NULL CHECK (tab_id > 0),
    origin         TEXT NOT NULL,
    state          TEXT NOT NULL CHECK (state IN ('parked', 'active', 'closed', 'lost')),
    updated_at     TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_application_tabs_live_tab
    ON application_tabs(tab_id) WHERE state IN ('parked', 'active');
ALTER TABLE application_fills ADD COLUMN submit_observed_at TEXT NOT NULL DEFAULT '';
