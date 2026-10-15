-- 099_stamp_legacy_profile_ids.sql
-- Strict explicit profiles: code no longer treats an empty profile_id as
-- 'default' (no COALESCE(profile_id,'default'), no implicit fallback), so
-- every legacy row that carries a NULL or '' owner is stamped once, here.
--
-- stamp = the profile whose id is 'default' if it exists, else the oldest
-- profile (created_at, then id). With no profiles at all there is no owner to
-- give, so every statement is a no-op (the EXISTS guard).
--
-- UPDATE OR IGNORE: a stamped row that would collide with a UNIQUE/PRIMARY
-- KEY row the stamp profile already owns is left as it was instead of
-- aborting the migration (and with it startup). Idempotent: a second run
-- finds no NULL/'' rows.
--
-- vault_keys (the only nullable profile_id) and ai_chat_messages are not
-- stamped here: their profile_id column may not exist yet (a singleton-shape
-- vault_keys, a pre-095 ai_chat_messages), which would abort this file.
-- ReconcileSchema stamps them in Go once the column exists.

UPDATE OR IGNORE agent_usage SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE applications SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE chat_memory_messages SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE connections SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE crawler_sessions SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE data_tables SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE evaluation_runs SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE hil_pending SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE jev_usage SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE people SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE person_links SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE person_messages SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE person_status_updates SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE platform_oauth_credentials SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE publications SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE send_allow SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE social_lists SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE tags SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE threads SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE vault_documents SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE vault_images SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE vault_secrets SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE workflow_executions SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE workflow_versions SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);
UPDATE OR IGNORE workflows SET profile_id = (SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1) WHERE (profile_id IS NULL OR profile_id = '') AND EXISTS (SELECT 1 FROM profiles);

-- The expression index from 026 served COALESCE(profile_id,'default') = ?,
-- which no query uses any more. 011's plain idx_workflows_profile covers
-- profile_id = ?.
DROP INDEX IF EXISTS idx_workflows_profile_default;
