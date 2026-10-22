-- Documents a user removed from MonoAgent although their file is still in the
-- profile's folder (`profile documents remove`). The folder scan registers every
-- document it finds; it skips a path listed here, so a removed document is not
-- added back by the next scan. `profile documents restore` deletes the row.
--
-- Additive only: one new table, nothing existing is read or changed.
-- path is the absolute path, as vault_documents.path holds it.
CREATE TABLE IF NOT EXISTS vault_document_exclusions (
    profile_id TEXT NOT NULL,
    path       TEXT NOT NULL,
    filename   TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (profile_id, path)
);
