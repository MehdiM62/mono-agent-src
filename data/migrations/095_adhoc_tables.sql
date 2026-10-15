-- 095_adhoc_tables.sql
--
-- Tables that until now were created only by Go code at runtime, outside
-- data/migrations/, so a database built from migrations alone lacked them:
--   ai_chat_messages     internal/ai/store.go (initTables)
--   ai_chat_answers      internal/ai/chat_answers.go (ensureAnswersTable)
--   connections          internal/connections/storage.go (createConnectionsTable,
--                        plus EnsureTable's vault_ref column)
--   oauth_refresh_locks  internal/connections/storage.go (createRefreshLocksTable)
--
-- The DDL matches the Go code exactly, with IF NOT EXISTS, so this is a
-- no-op on every database where that code already ran. connections includes
-- vault_ref (added by EnsureTable's ALTER) as its last column, which is
-- where the ALTER puts it on existing databases too.
--
-- Columns that later code adds with ALTER TABLE ... ADD COLUMN (SQLite has
-- no ADD COLUMN IF NOT EXISTS) are healed by storage.ReconcileSchema, which
-- runs at the end of every ApplyMigrations:
--   connections.vault_ref, ai_chat_conversations.{mode, cwd, effort, org_mode},
--   ai_chat_messages.{tool_call_id, session_id, profile_id} (older tables)
--
-- The other runtime-created tables are already created by migrations:
-- ai_chat_conversations/ai_chat_turns/ai_chat_events (040),
-- platform_oauth_credentials (013), vault_keys (017/027).

CREATE TABLE IF NOT EXISTS ai_chat_messages (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    tool_calls TEXT NOT NULL DEFAULT '',
    tool_call_id TEXT NOT NULL DEFAULT '',
    provider_id TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    token_count INTEGER NOT NULL DEFAULT 0,
    session_id TEXT NOT NULL DEFAULT '',
    -- '' (not 'default'): rows predating the column match no profile; see
    -- internal/ai/store.go.
    profile_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ai_chat_answers (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id      TEXT NOT NULL,
    conversation_id TEXT NOT NULL,
    turn_id         TEXT NOT NULL,
    agent_id        TEXT NOT NULL,
    question_id     TEXT NOT NULL,
    text            TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    consumed_at     TEXT NOT NULL DEFAULT '',
    UNIQUE(profile_id, turn_id, agent_id, question_id)
);

CREATE TABLE IF NOT EXISTS connections (
    id          TEXT PRIMARY KEY,
    platform    TEXT NOT NULL,
    method      TEXT NOT NULL,
    label       TEXT NOT NULL,
    account_id  TEXT NOT NULL DEFAULT '',
    data        TEXT NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'active',
    last_tested TEXT NOT NULL DEFAULT '',
    profile_id  TEXT NOT NULL DEFAULT 'default',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    vault_ref   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_connections_platform ON connections(platform);
CREATE INDEX IF NOT EXISTS idx_connections_status   ON connections(status);
CREATE INDEX IF NOT EXISTS idx_connections_profile  ON connections(profile_id);

CREATE TABLE IF NOT EXISTS oauth_refresh_locks (
    connection_id TEXT PRIMARY KEY,
    locked_at     TEXT NOT NULL
);
