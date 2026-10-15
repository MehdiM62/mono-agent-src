-- 096_drop_legacy_credentials.sql
--
-- The credentials table (006_workflow_system.sql, profile_id added in
-- 011_profiles.sql) predates the secrets vault and has a plaintext `data`
-- column. Nothing reads or writes it any more: credentials live in the vault
-- (vault_secrets) and connections. Dropping it also drops its indexes
-- (idx_credentials_type, idx_credentials_profile).
--
-- Irreversible: ApplyMigrations writes a pre-migrate snapshot to
-- ~/.monoagent/backups/ before running pending migrations on a database
-- that holds data, so a copy survives.
--
-- Kept on purpose (still referenced by code): vault_keys_legacy
-- (internal/secrets/dek.go migrates the old singleton key from it), posts and
-- post_comments (written by `node run` scrapers, read by `people posts`).

DROP TABLE IF EXISTS credentials;
