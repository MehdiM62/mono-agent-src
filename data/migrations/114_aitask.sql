-- AI tasks (`monoagentcli aitask`): a goal handed to a coder conversation, with a
-- result the agent hands back through `aitask complete`.
--
-- Additive only: one new table, nothing existing is read or changed.
--
-- status is queued, running, needs_user, complete, failed or cancelled. token_hash
-- is the hex SHA-256 of the token the task's turn runs with (only its holder may
-- complete or fail the task); the token itself is never stored. application_id is
-- empty unless the task is about an application. prompt is the first turn's prompt.
CREATE TABLE IF NOT EXISTS ai_tasks (
    id              TEXT PRIMARY KEY,
    profile_id      TEXT NOT NULL,
    kind            TEXT NOT NULL,
    conversation_id TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL,
    goal            TEXT NOT NULL,
    application_id  TEXT NOT NULL DEFAULT '',
    access          TEXT NOT NULL DEFAULT '',
    runtime         TEXT NOT NULL DEFAULT '',
    unattended      INTEGER NOT NULL DEFAULT 0,
    prompt          TEXT NOT NULL DEFAULT '',
    token_hash      TEXT NOT NULL DEFAULT '',
    result_json     TEXT NOT NULL DEFAULT '',
    error           TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ai_tasks_profile_status ON ai_tasks(profile_id, status);
CREATE INDEX IF NOT EXISTS idx_ai_tasks_application ON ai_tasks(profile_id, application_id);
