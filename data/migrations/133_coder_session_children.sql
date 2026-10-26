-- 133_coder_session_children.sql
-- Master coder sessions create and control their descendant sessions (P4 of
-- docs/mastermind/plans/2026-10-26-coder-session-host.md). Additive only.
--
-- budget_usd: the spend cap carved out of the parent's for a child session (0 = none).
-- coder_session_tokens: the caller identity of a host-started turn; only the SHA-256
--   of the token is stored, and it names one profile, conversation and turn.
-- coder_session_spawns: one row per child a session created, for the spawn rate limit.
ALTER TABLE coder_sessions ADD COLUMN budget_usd REAL NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS coder_session_tokens (
    token_hash      TEXT PRIMARY KEY,
    profile_id      TEXT NOT NULL,
    conversation_id TEXT NOT NULL,
    turn_id         TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    expires_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_coder_session_tokens_conv ON coder_session_tokens(profile_id, conversation_id);

CREATE TABLE IF NOT EXISTS coder_session_spawns (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id      TEXT NOT NULL,
    parent          TEXT NOT NULL,
    child           TEXT NOT NULL,
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_coder_session_spawns_parent ON coder_session_spawns(profile_id, parent, id);
