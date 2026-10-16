-- Workflow definitions the operator approved (#831 review C). A run of any
-- other definition — one the chat, an agent or a workflow made or changed
-- since the operator last saved or activated the workflow from the app or
-- their own terminal — is an AI run, whatever triggered it
-- (internal/workflow/approvals.go).
--
--   workflow_id      the workflow; its approvals go with it when it is deleted
--   definition_hash  the hash of an approved definition (the versions' hash)
--
-- Every definition a version recorded before this migration (a save, or a
-- run that pinned it) counts as approved; the current definition of every
-- workflow is approved too, by a one-time Go step on the first open after
-- this migration (internal/workflow/approvals_backfill.go), since it may be
-- in a file this SQL can't read.
CREATE TABLE IF NOT EXISTS workflow_approvals (
    workflow_id     TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    definition_hash TEXT NOT NULL,
    approved_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (workflow_id, definition_hash)
);

INSERT OR IGNORE INTO workflow_approvals (workflow_id, definition_hash)
    SELECT DISTINCT workflow_id, definition_hash FROM workflow_versions
    WHERE workflow_id IN (SELECT id FROM workflows);
