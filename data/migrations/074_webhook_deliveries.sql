-- Delivery IDs a workflow's webhook has accepted, for the trigger.webhook
-- dedupe_header option: a redelivery with a remembered ID is answered with the
-- first delivery's execution_id instead of starting another run. execution_id
-- is '' while the first delivery is still starting its run. Rows older than
-- seven days are pruned by the webhook server.
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    workflow_id  TEXT NOT NULL,
    delivery_id  TEXT NOT NULL,
    execution_id TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (workflow_id, delivery_id)
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_created_at ON webhook_deliveries (created_at);
