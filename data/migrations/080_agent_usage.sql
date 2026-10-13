-- 080_agent_usage.sql
--
-- Token and cost use of agent turns (agent.ask now; chat turns later), one
-- row per turn. Token and cost columns are NULL when the runtime did not
-- report them, which is not the same as zero.
CREATE TABLE IF NOT EXISTS agent_usage (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    at            TEXT    NOT NULL,
    profile_id    TEXT    NOT NULL DEFAULT 'default',
    source        TEXT    NOT NULL, -- agent.ask | chat | …
    workflow_id   TEXT    NOT NULL DEFAULT '',
    execution_id  TEXT    NOT NULL DEFAULT '',
    node_id       TEXT    NOT NULL DEFAULT '',
    session_id    TEXT    NOT NULL DEFAULT '',
    runtime       TEXT    NOT NULL,
    model         TEXT    NOT NULL DEFAULT '',
    input_tokens  INTEGER,
    output_tokens INTEGER,
    cost_usd      REAL
);

CREATE INDEX IF NOT EXISTS idx_agent_usage_at ON agent_usage(at);
CREATE INDEX IF NOT EXISTS idx_agent_usage_execution ON agent_usage(execution_id);
