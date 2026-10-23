-- Tags on profile documents. Documents share the profile's tags table with
-- people, so a tag called "contract" is one tag wherever it is used.
--
-- Additive only: one new link table, nothing existing is read or changed.
-- Removing a document or a tag drops its links.
CREATE TABLE IF NOT EXISTS document_tags (
    document_id TEXT NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
    tag_id      TEXT NOT NULL REFERENCES tags(id)            ON DELETE CASCADE,
    PRIMARY KEY (document_id, tag_id)
);
CREATE INDEX IF NOT EXISTS idx_document_tags_tag ON document_tags(tag_id);
