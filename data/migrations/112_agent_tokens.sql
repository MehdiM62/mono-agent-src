-- Scoped, expiring, revocable bearer tokens for the profile-scoped HTTP API
-- (`monoagentcli agent-mode token`), for a coding agent that runs in a sandbox.
--
-- Additive only: two new tables, nothing existing is read or changed. A profile's
-- own bearer token (the vault entry) keeps working as scope "full".
--
-- token_hash is the hex SHA-256 of the token; the verifier looks it up and compares
-- it in constant time. scope is read, write, run or full (the API decides what
-- each one may call). expires_at and revoked_at are UTC timestamps; a token is
-- valid only while neither has passed. A name is unique per profile among tokens
-- that are not revoked, so a revoked token's name can be reused.
CREATE TABLE IF NOT EXISTS agent_tokens (
    id           TEXT PRIMARY KEY,
    profile_id   TEXT NOT NULL,
    name         TEXT NOT NULL,
    scope        TEXT NOT NULL,
    prefix       TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    created_at   TEXT NOT NULL,
    expires_at   TEXT,
    last_used_at TEXT,
    revoked_at   TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_tokens_profile_name_active
    ON agent_tokens(profile_id, name) WHERE revoked_at IS NULL;

-- One row per request made with an agent token: what it asked for and what it got.
-- The token itself is never written here, only its name.
CREATE TABLE IF NOT EXISTS agent_token_audit (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    at         TEXT NOT NULL,
    token_id   TEXT NOT NULL,
    token_name TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    method     TEXT NOT NULL,
    path       TEXT NOT NULL,
    status     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_token_audit_token ON agent_token_audit(token_id, id);
