-- Webhooks registered with an app's API when a workflow is activated
-- (internal/workflow/webhook_registrar.go): one row per trigger node, so a
-- later activation reuses the vendor's hook and a deactivation, in any
-- process, can delete it. config is the node config the hook was registered
-- with (without credential data); config_hash detects a changed node.
CREATE TABLE IF NOT EXISTS app_webhooks (
    workflow_id TEXT NOT NULL,
    node_id     TEXT NOT NULL,
    registrar   TEXT NOT NULL,
    hook_id     TEXT NOT NULL DEFAULT '',
    url         TEXT NOT NULL DEFAULT '',
    secret      TEXT NOT NULL DEFAULT '',
    config      TEXT NOT NULL DEFAULT '{}',
    config_hash TEXT NOT NULL DEFAULT '',
    extra       TEXT NOT NULL DEFAULT '{}',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (workflow_id, node_id)
);
