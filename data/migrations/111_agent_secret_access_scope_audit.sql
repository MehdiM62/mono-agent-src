-- Agent secret access, narrowed and audited (review of #943). A row of
-- profile_agent_secret_access (110) still means on. The new columns narrow
-- it: secret_names (JSON list; NULL = every secret), hosts (JSON list of the
-- hosts a resolved secret may be sent to), any_host (1 = no host binding:
-- what rows made before this migration had) and expires_at (NULL = never;
-- a past time = off). Additive: existing rows keep their old meaning.
ALTER TABLE profile_agent_secret_access ADD COLUMN secret_names TEXT;
ALTER TABLE profile_agent_secret_access ADD COLUMN hosts TEXT;
ALTER TABLE profile_agent_secret_access ADD COLUMN any_host INTEGER NOT NULL DEFAULT 1;
ALTER TABLE profile_agent_secret_access ADD COLUMN expires_at TIMESTAMP;

-- Every secret an unapproved run read while access was on: which workflow,
-- run and step, which secret by name (never its value), and when.
CREATE TABLE IF NOT EXISTS profile_agent_secret_audit (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id   TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    at           TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    workflow_id  TEXT NOT NULL DEFAULT '',
    execution_id TEXT NOT NULL DEFAULT '',
    node_id      TEXT NOT NULL DEFAULT '',
    node_name    TEXT NOT NULL DEFAULT '',
    secret_name  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_secret_audit_profile ON profile_agent_secret_audit(profile_id, id);
