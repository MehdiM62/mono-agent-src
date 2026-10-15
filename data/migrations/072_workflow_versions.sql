-- Workflow definition history (#21). Every create/save of a workflow whose
-- definition changed records a snapshot here (name, description, nodes,
-- connections as JSON); definition_hash is the hash of the snapshot's
-- canonical form and is what consecutive identical saves are deduped on.
-- Nothing is pruned automatically: `workflow versions prune --keep N` does it.
CREATE TABLE IF NOT EXISTS workflow_versions (
    workflow_id     TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    version         INTEGER NOT NULL,
    snapshot        TEXT NOT NULL,
    definition_hash TEXT NOT NULL,
    profile_id      TEXT NOT NULL DEFAULT 'default',
    note            TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (workflow_id, version)
);

-- The definition version an execution was created against. A run (and a
-- WAITING run's resume) executes that snapshot rather than whatever the
-- workflow looks like at dispatch. NULL: created before this migration, or
-- the version could not be recorded — such runs use the current definition.
ALTER TABLE workflow_executions ADD COLUMN workflow_version INTEGER;
