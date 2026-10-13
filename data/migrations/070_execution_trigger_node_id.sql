-- 070_execution_trigger_node_id.sql
-- Records which trigger node fired an execution. Until now it lived only in
-- the in-memory queue request, so a run that was adopted (--no-wait), resumed
-- after a Human-in-Loop pause, or retried lost it and fired every trigger
-- branch of a multi-trigger workflow. Empty means "every trigger node"
-- (manual runs), the behaviour of rows written before this column existed.
ALTER TABLE workflow_executions ADD COLUMN trigger_node_id TEXT NOT NULL DEFAULT '';
