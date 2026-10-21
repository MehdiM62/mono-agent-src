-- A profile whose owner let agent-written workflows read its vault: a run of
-- a definition the operator never approved resolves @secret references
-- instead of refusing them (internal/secrets/agent_access.go). A row means
-- on; none is the default. Only the operator's own terminal writes it
-- (`monoagentcli secret agent-access on`), and it lives outside any workflow
-- definition, so no import or settings key can claim it.
CREATE TABLE IF NOT EXISTS profile_agent_secret_access (
    profile_id TEXT PRIMARY KEY REFERENCES profiles(id) ON DELETE CASCADE,
    enabled_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
