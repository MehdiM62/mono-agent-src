-- Error classes (#30): every failed node records what kind of failure it was
-- (invalid_config, not_setup, browser_unavailable, selector_missing, auth,
-- rate_limited, upstream_5xx, timeout, rejected, cancelled, unknown) and a
-- one-line fix hint; the execution keeps the class of its first failure.
ALTER TABLE workflow_execution_nodes ADD COLUMN error_class TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_execution_nodes ADD COLUMN error_hint TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_executions ADD COLUMN error_class TEXT NOT NULL DEFAULT '';

-- Workflow-level settings (#18), e.g. {"error_workflow": "<id>"}: the
-- workflow that runs when this one fails. The file store keeps the same
-- object in the workflow's JSON.
ALTER TABLE workflows ADD COLUMN settings TEXT NOT NULL DEFAULT '{}';
