-- Per-destination "always allow" for outbound sends prepared by agents (sending
-- waits for approval by default). Only the operator adds rows: the CLI refuses
-- `hil allow` from an agent context. recipient is the lower-cased person id,
-- profile URL, address or name the approval card showed.
CREATE TABLE IF NOT EXISTS send_allow (
    id         TEXT PRIMARY KEY,
    profile_id TEXT NOT NULL DEFAULT 'default',
    action     TEXT NOT NULL DEFAULT '',
    platform   TEXT NOT NULL DEFAULT '',
    account    TEXT NOT NULL DEFAULT '',
    recipient  TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (profile_id, action, platform, account, recipient)
);
