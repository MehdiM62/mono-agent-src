-- Files the browser extension saves into a profile's vault (vault.save, #451:
-- the side panel's confirm card, internal/extension/vault_save.go).
--
--   client_id   the extension's id for one save. A retry of the same save names
--               the same id and gets the item it already made, so a save lands
--               once per profile however often it is sent. NULL for every other
--               way into the vault.
--   source_url  the page or file address the extension saved it from, without
--               user-info; NULL when it sent none and for every other item.
ALTER TABLE vault_images ADD COLUMN client_id TEXT;
ALTER TABLE vault_images ADD COLUMN source_url TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_vault_images_client
    ON vault_images(profile_id, client_id) WHERE client_id IS NOT NULL;
