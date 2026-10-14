-- 079_data_tables.sql
--
-- Data tables: n8n-style tables a profile keeps across workflow runs, which
-- the core.data_table node and `monoagentcli table` read and write.
-- columns is a JSON list of {name, type} (type string | number | boolean |
-- date); a row's data is a JSON object of its column values. Row ids are
-- numbers, as in n8n.
CREATE TABLE IF NOT EXISTS data_tables (
    id          TEXT PRIMARY KEY,
    profile_id  TEXT NOT NULL DEFAULT 'default',
    name        TEXT NOT NULL,
    columns     TEXT NOT NULL DEFAULT '[]',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE (profile_id, name)
);

CREATE TABLE IF NOT EXISTS data_table_rows (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    table_id    TEXT NOT NULL REFERENCES data_tables(id) ON DELETE CASCADE,
    data        TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_data_table_rows_table ON data_table_rows(table_id);
