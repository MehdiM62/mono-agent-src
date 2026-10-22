-- AI task budgets, worker pool (`internal/aitask/budget`): the share of a task
-- turn's reservation its dynamic-org workers draw from. Each worker run reserves
-- part of the pool before it starts (one conditional INSERT, so workers running
-- together cannot reserve more than the pool) and settles what it spent when it
-- ends. pool_key names the turn (task id and chat turn id).
-- Additive only: one new table.
CREATE TABLE IF NOT EXISTS ai_budget_pool (
    res_id       TEXT PRIMARY KEY,
    pool_key     TEXT NOT NULL,
    reserved_usd REAL NOT NULL,
    spent_usd    REAL NOT NULL DEFAULT 0,
    settled      INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ai_budget_pool_key ON ai_budget_pool(pool_key);
