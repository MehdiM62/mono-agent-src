package secrets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

// A wrapped DEK is bound to the install it was made in (#903): its AES-GCM seal carries the
// canonical path of the database file as additional authenticated data. The key that unwraps it
// lives in the OS key store, which gives it to every HOME of one OS user, so without the binding a
// copy of monoagent.db in another HOME (made by an agent, a script, a restored backup) unwraps
// every credential. With it, the copy's DEK fails to open at any other path.
//
// This closes the accidental-copy class only. A process that computes the original path's binding
// on purpose (or reads the real database) has what the user has; only an OS boundary stops that
// (SECURITY.md, "A copy of ~/.monoagent and the key store").
//
// A DEK wrapped by an earlier release has no binding. It is rewrapped on the first open at this
// OS user's own install and refused anywhere else until `secret rebind` says the move is
// intended.

// ErrNotBound is a vault whose DEK is not bound to the database it was opened from.
var ErrNotBound = errors.New("secrets: this vault was made for another install")

const bindingTag = "monoagent-vault-install/v1\x00"

// ownInstallDB is this OS user's own database file, per the system's user database (never
// $HOME), "" when the system has none. A test replaces it.
var ownInstallDB = func() string {
	u, err := user.Current()
	if err != nil || u.HomeDir == "" {
		return ""
	}
	return filepath.Join(u.HomeDir, ".monoagent", "monoagent.db")
}

// canonicalPath is p made absolute with symlinks resolved, so one install has one name however it
// is reached. A file that does not exist yet is named by its resolved directory.
func canonicalPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r
	}
	if r, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		return filepath.Join(r, filepath.Base(abs))
	}
	return abs
}

// installPath is the canonical path of the database file db is open on, "" for an in-memory or
// temporary database (nothing to copy, so nothing to bind).
func installPath(ctx context.Context, db *sql.DB) (string, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA database_list`)
	if err != nil {
		return "", fmt.Errorf("secrets: finding the database file: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			return "", fmt.Errorf("secrets: finding the database file: %w", err)
		}
		if name == "main" {
			if file == "" {
				return "", nil
			}
			return canonicalPath(file), nil
		}
	}
	return "", rows.Err()
}

// bindingAD is the additional data a DEK wrapped for the install at path carries; nil for none.
func bindingAD(path string) []byte {
	if path == "" {
		return nil
	}
	return []byte(bindingTag + path)
}

// wrapDEK seals dek under kek, bound to the install db is open on.
func wrapDEK(ctx context.Context, db *sql.DB, kek, dek []byte) (wrapped, nonce []byte, err error) {
	path, err := installPath(ctx, db)
	if err != nil {
		return nil, nil, err
	}
	return encryptAD(kek, dek, bindingAD(path))
}

// isOwnInstall says whether path is this OS user's own database file.
func isOwnInstall(path string) bool {
	own := ownInstallDB()
	if own == "" || path == "" {
		return false
	}
	return path == canonicalPath(own)
}

// unwrapDEK opens a wrapped DEK read from vault_keys for profileID. A DEK bound to this install
// opens as is. One with no binding (wrapped by an earlier release) opens only in this OS user's
// own install, where it is rewrapped bound; anywhere else it is ErrNotBound. rewrite is false
// for a read-only probe (CheckVault).
func unwrapDEK(ctx context.Context, db *sql.DB, kek, wrapped, nonce []byte, profileID string, rewrite bool) ([]byte, error) {
	path, err := installPath(ctx, db)
	if err != nil {
		return nil, err
	}
	ad := bindingAD(path)
	dek, err := decryptAD(kek, wrapped, nonce, ad)
	if err == nil || ad == nil {
		return dek, err
	}
	legacy, lerr := Decrypt(kek, wrapped, nonce)
	if lerr != nil {
		// Neither this install's binding nor none: another install's, or a wrong key.
		return nil, err
	}
	if !isOwnInstall(path) {
		return nil, fmt.Errorf("%w: its key was not made for %s (to adopt it here on purpose, run `monoagentcli secret rebind`)", ErrNotBound, path)
	}
	if rewrite {
		if rerr := rewrapDEK(ctx, db, kek, legacy, profileID, path); rerr != nil {
			return nil, rerr
		}
	}
	return legacy, nil
}

// rewrapDEK stores dek in profileID's vault_keys row wrapped under kek and bound to path.
func rewrapDEK(ctx context.Context, db *sql.DB, kek, dek []byte, profileID, path string) error {
	wrapped, nonce, err := encryptAD(kek, dek, bindingAD(path))
	if err != nil {
		return fmt.Errorf("secrets: binding the key to %s: %w", path, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE vault_keys SET wrapped_dek = ?, wrapped_nonce = ? WHERE profile_id = ?`, wrapped, nonce, profileID); err != nil {
		return fmt.Errorf("secrets: binding the key to %s: %w", path, err)
	}
	return nil
}

// Rebind binds profileID's vault to the install db is open on, for a move the user made on
// purpose (a renamed HOME, a database restored to a new place). The key is opened as the install
// it was made for (from, that install's database path as it was) or, when from is "", as one with
// no binding. It reports what it did; the vault key in the OS key store must be there already.
func Rebind(ctx context.Context, db *sql.DB, profileID, from string) (string, error) {
	if profileID == "" {
		return "", errors.New("secrets: no profile to rebind")
	}
	path, err := installPath(ctx, db)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", errors.New("secrets: this database has no file to bind to")
	}
	kek, found, err := peekKEK(profileID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("secrets: this profile's vault key is not in the key store, so there is nothing to rebind it with")
	}
	var wrapped, nonce []byte
	err = db.QueryRowContext(ctx, `SELECT wrapped_dek, wrapped_nonce FROM vault_keys WHERE profile_id = ?`, profileID).Scan(&wrapped, &nonce)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("secrets: this profile has no vault key")
	}
	if err != nil {
		return "", fmt.Errorf("secrets: reading vault_keys: %w", err)
	}
	if dek, derr := decryptAD(kek, wrapped, nonce, bindingAD(path)); derr == nil {
		_ = dek
		return "already bound to " + path, nil
	}
	var oldAD []byte
	if from != "" {
		if _, serr := os.Stat(filepath.Dir(from)); serr == nil {
			from = canonicalPath(from)
		}
		oldAD = bindingAD(from)
	}
	dek, err := decryptAD(kek, wrapped, nonce, oldAD)
	if err != nil {
		if from == "" {
			return "", errors.New("secrets: this vault key is bound to another install; pass --from <the old monoagent.db path>")
		}
		return "", fmt.Errorf("secrets: this vault key was not bound to %s", from)
	}
	if err := rewrapDEK(ctx, db, kek, dek, profileID, path); err != nil {
		return "", err
	}
	dekEntriesMu.Lock()
	delete(dekEntries, dekKey{db: db, profileID: profileID})
	dekEntriesMu.Unlock()
	return "bound to " + path, nil
}
