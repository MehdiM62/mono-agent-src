-- An idempotency key for task sessions, like tasks.client_id (062): a retried or
-- double-clicked "session new" with the same key in the same profile returns the
-- existing session. Additive only; sessions without a key are unaffected.
ALTER TABLE task_sessions ADD COLUMN client_id TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_task_sessions_client ON task_sessions(profile_id, client_id) WHERE client_id IS NOT NULL;
