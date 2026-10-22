-- AI task budgets (`monoagentcli aitask budget`): spend caps the operator sets per
-- kind, conversation, role or globally, and the ledger every capped task is
-- charged to.
--
-- Additive only: three new tables, nothing existing is read or changed.
--
-- ai_budgets: scope is kind, conversation, role or global (key is '' for global).
-- cap_usd is a ceiling, and 0 means no spend, never unlimited. Raising or changing a
-- cap is the operator's; an agent may only lower its own conversation's
-- (updated_by says which of the two last wrote the row).
-- ai_budget_reservations: one row per task that has started, holding its cap
-- while it runs. A start is admitted only if the reservations and spend of every
-- scope it falls in still fit under that scope's cap, decided in one statement.
-- released is 1 once the task is over and only what it spent counts.
-- ai_budget_spend: what the task's conversation spent, per agent (the lead has
-- agent_id ''; a dynamic-org worker has its own, with its role), summed over
-- the whole conversation so a worker spawned to dodge a cap is still charged.
CREATE TABLE IF NOT EXISTS ai_budgets (
    scope      TEXT NOT NULL,
    key        TEXT NOT NULL DEFAULT '',
    cap_usd    REAL NOT NULL,
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL,
    PRIMARY KEY (scope, key)
);
CREATE TABLE IF NOT EXISTS ai_budget_reservations (
    task_id         TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    kind            TEXT NOT NULL,
    reserved_usd    REAL NOT NULL,
    released        INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ai_budget_reservations_conv ON ai_budget_reservations(conversation_id);
CREATE TABLE IF NOT EXISTS ai_budget_spend (
    task_id    TEXT NOT NULL,
    agent_id   TEXT NOT NULL DEFAULT '',
    role       TEXT NOT NULL DEFAULT '',
    spent_usd  REAL NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (task_id, agent_id)
);
