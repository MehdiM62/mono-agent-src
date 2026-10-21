-- vault_key_hints (#903): the path a profile's vault key was bound to, a non-secret aid.
-- A copied database cannot open the vault (the key is bound to the original path); this
-- tells it where the key was made, so the error, `doctor` and `secret rebind --from` can
-- name the path. It is only ever tried as a guess at decrypting, so a wrong or forged
-- hint changes nothing. A separate table keeps vault_keys (reconciled in Go) untouched.
CREATE TABLE IF NOT EXISTS vault_key_hints (
    profile_id TEXT PRIMARY KEY,
    bound_path TEXT NOT NULL
);
