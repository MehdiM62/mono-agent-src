-- Replies a paused node waits for over HTTP (/reply/<token> on the webhook
-- server): approve/decline links, a free-text or custom form, or a call to a
-- core.wait resume URL. One row per run of the node in its execution
-- (run_index), so a node inside a loop asks again on every iteration.
-- status is pending until answered (answer holds the reply as JSON) or
-- expired; consumed is set once the node's re-run has read the outcome.
CREATE TABLE IF NOT EXISTS wait_replies (
    execution_id TEXT NOT NULL,
    node_id      TEXT NOT NULL,
    run_index    INTEGER NOT NULL,
    workflow_id  TEXT NOT NULL DEFAULT '',
    token        TEXT NOT NULL,
    kind         TEXT NOT NULL,
    spec         TEXT NOT NULL DEFAULT '{}',
    status       TEXT NOT NULL DEFAULT 'pending',
    answer       TEXT NOT NULL DEFAULT '',
    sent         INTEGER NOT NULL DEFAULT 0,
    consumed     INTEGER NOT NULL DEFAULT 0,
    expires_at   TEXT,
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    answered_at  TIMESTAMP,
    PRIMARY KEY (execution_id, node_id, run_index)
);

CREATE INDEX IF NOT EXISTS idx_wait_replies_token ON wait_replies (token);
