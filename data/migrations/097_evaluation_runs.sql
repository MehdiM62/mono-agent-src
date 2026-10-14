-- 097_evaluation_runs.sql
--
-- Evaluation runs: `monoagentcli workflow evaluate` runs a workflow once per
-- row of its trigger.evaluation dataset (n8n's evaluations). A run keeps the
-- mean of each metric over its cases; a case is one dataset row, the
-- execution it ran as, and the inputs, outputs and metrics the workflow's
-- core.evaluation nodes recorded. inputs, outputs, metrics and dataset are
-- JSON objects.
CREATE TABLE IF NOT EXISTS evaluation_runs (
    id             TEXT PRIMARY KEY,
    profile_id     TEXT NOT NULL DEFAULT 'default',
    workflow_id    TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'new',
    error_code     TEXT NOT NULL DEFAULT '',
    error_message  TEXT NOT NULL DEFAULT '',
    dataset        TEXT NOT NULL DEFAULT '{}',
    metrics        TEXT NOT NULL DEFAULT '{}',
    created_at     TEXT NOT NULL,
    finished_at    TEXT
);

CREATE INDEX IF NOT EXISTS idx_evaluation_runs_workflow ON evaluation_runs(profile_id, workflow_id, created_at);

CREATE TABLE IF NOT EXISTS evaluation_results (
    id             TEXT PRIMARY KEY,
    run_id         TEXT NOT NULL REFERENCES evaluation_runs(id) ON DELETE CASCADE,
    position       INTEGER NOT NULL,
    row_id         TEXT NOT NULL DEFAULT '',
    row_number     INTEGER NOT NULL DEFAULT 0,
    execution_id   TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'new',
    error_code     TEXT NOT NULL DEFAULT '',
    error_message  TEXT NOT NULL DEFAULT '',
    inputs         TEXT NOT NULL DEFAULT '{}',
    outputs        TEXT NOT NULL DEFAULT '{}',
    metrics        TEXT NOT NULL DEFAULT '{}',
    created_at     TEXT NOT NULL,
    finished_at    TEXT
);

CREATE INDEX IF NOT EXISTS idx_evaluation_results_run ON evaluation_results(run_id, position);
