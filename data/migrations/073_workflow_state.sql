-- Per-workflow state that outlives a single execution (internal/workflow/staticdata.go).
--
-- node_id is the owning node's id, or '' for state shared by the whole
-- workflow. Neither table has a foreign key to workflows: a workflow can live
-- in the file store only, so WorkflowEngine.DeleteWorkflow removes the rows
-- explicitly instead.

-- Dedupe values a node has already let through, stored as sha256 hex.
-- seen_at is unix nanoseconds and is refreshed every time the value is seen
-- again, so TTL and history-size eviction drop the values seen least recently.
CREATE TABLE IF NOT EXISTS workflow_seen_keys (
    workflow_id TEXT NOT NULL,
    node_id     TEXT NOT NULL DEFAULT '',
    key_hash    TEXT NOT NULL,
    seen_at     INTEGER NOT NULL,
    PRIMARY KEY (workflow_id, node_id, key_hash)
);
CREATE INDEX IF NOT EXISTS workflow_seen_keys_age ON workflow_seen_keys(workflow_id, node_id, seen_at);

-- Key/value static data (JSON values), e.g. a feed reader's since-last-run cursor.
CREATE TABLE IF NOT EXISTS workflow_static_data (
    workflow_id TEXT NOT NULL,
    node_id     TEXT NOT NULL DEFAULT '',
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,
    updated_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (workflow_id, node_id, key)
);
