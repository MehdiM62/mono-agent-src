-- App webhooks that expire (Microsoft Graph subscriptions, Jira Cloud
-- webhooks): the daemon renews them before expires_at
-- (internal/workflow/webhook_renew.go). expires_at NULL never expires;
-- renewed_at is when the current expiry was set. renew_error and
-- renew_error_class keep the last failed renewal, '' after a success.
ALTER TABLE app_webhooks ADD COLUMN expires_at TIMESTAMP;
ALTER TABLE app_webhooks ADD COLUMN renewed_at TIMESTAMP;
ALTER TABLE app_webhooks ADD COLUMN renew_error TEXT NOT NULL DEFAULT '';
ALTER TABLE app_webhooks ADD COLUMN renew_error_class TEXT NOT NULL DEFAULT '';
