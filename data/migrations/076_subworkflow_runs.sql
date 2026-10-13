-- Links a core.execute_workflow node to the child executions it started.
-- The parent re-reads its children from here when it resumes, and a child
-- reaching a terminal status looks its parent up here to wake it.
CREATE TABLE IF NOT EXISTS subworkflow_runs (
    parent_execution_id TEXT NOT NULL,
    parent_node_id      TEXT NOT NULL,
    child_execution_id  TEXT NOT NULL,
    item_index          INTEGER NOT NULL DEFAULT -1,
    wait                INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (parent_execution_id, parent_node_id, child_execution_id)
);
CREATE INDEX IF NOT EXISTS idx_subworkflow_runs_child ON subworkflow_runs(child_execution_id);
