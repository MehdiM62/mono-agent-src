package vault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SaveInput is one file SaveBytes stores, already checked by its caller
// (the extension bridge's vault.save, internal/extension/vault_save.go).
type SaveInput struct {
	Data []byte
	// Ext is the file's extension with its dot, chosen by the caller from the
	// checked media type, never from a name the sender chose.
	Ext string
	// Label is the name the item shows; Source says where it came from.
	Label  string
	Source string
	// ClientID makes the save idempotent within the profile; SourceURL is the
	// address it was saved from ("" for none).
	ClientID  string
	SourceURL string
}

// ErrNoClientID refuses a SaveBytes without a client id: without one a retry
// could store the same file twice.
var ErrNoClientID = errors.New("vault.SaveBytes: client id is required")

// SaveBytes stores in.Data in the profile's vault as a new item (vault_images,
// the vault `image list`, vault_item_list and vault_item_get_path read), at
// most once per client id: a client id this profile's vault already has gets
// that item back with created false, and nothing is written. The file is
// named after the item's id (img-NNN<ext>), so no name the sender chose ever
// reaches the filesystem. The check, the file and the row are one IMMEDIATE
// transaction, as in Register, so two saves of one client id at once store
// one item. Unlike Register it does not index the item in the knowledge
// graph: that shells out to monomind, which a bridge request must not wait on.
func SaveBytes(ctx context.Context, db *sql.DB, profileID string, in SaveInput) (id string, created bool, err error) {
	if db == nil {
		return "", false, fmt.Errorf("vault.SaveBytes: db is nil")
	}
	if strings.TrimSpace(profileID) == "" {
		return "", false, fmt.Errorf("vault.SaveBytes: %w", ErrNoProfile)
	}
	if in.ClientID == "" {
		return "", false, ErrNoClientID
	}
	if !strings.HasPrefix(in.Ext, ".") || strings.ContainsAny(in.Ext, `/\`) || strings.Contains(in.Ext, "..") {
		return "", false, fmt.Errorf("vault.SaveBytes: bad extension %q", in.Ext)
	}
	vaultDir := VaultDir(db, profileID)
	if !filepath.IsAbs(vaultDir) {
		return "", false, fmt.Errorf("vault.SaveBytes: no vault folder for this profile")
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return "", false, fmt.Errorf("vault.SaveBytes: get conn: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return "", false, fmt.Errorf("vault.SaveBytes: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	err = conn.QueryRowContext(ctx, `SELECT id FROM vault_images WHERE profile_id = ? AND client_id = ?`, profileID, in.ClientID).Scan(&id)
	switch {
	case err == nil:
		return id, false, nil
	case !errors.Is(err, sql.ErrNoRows):
		return "", false, fmt.Errorf("vault.SaveBytes: look up client id: %w", err)
	}

	var seq int
	if err := conn.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM vault_images`).Scan(&seq); err != nil {
		return "", false, fmt.Errorf("vault.SaveBytes: get next seq: %w", err)
	}
	id = fmt.Sprintf("img-%03d", seq)
	if err := EnsureVaultDir(db, profileID); err != nil {
		return "", false, fmt.Errorf("vault.SaveBytes: ensure vault dir: %w", err)
	}
	destFilename := id + in.Ext
	destPath := filepath.Join(vaultDir, destFilename)
	if err := writeNew(destPath, in.Data); err != nil {
		return "", false, fmt.Errorf("vault.SaveBytes: write file: %w", err)
	}
	nullStr := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	_, err = conn.ExecContext(ctx, `
		INSERT INTO vault_images (id, seq, path, filename, size_bytes, source, label, profile_id, client_id, source_url)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, seq, destPath, destFilename, len(in.Data), in.Source, nullStr(in.Label), profileID, in.ClientID, nullStr(in.SourceURL))
	if err != nil {
		_ = os.Remove(destPath)
		return "", false, fmt.Errorf("vault.SaveBytes: insert: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		_ = os.Remove(destPath)
		return "", false, fmt.Errorf("vault.SaveBytes: commit: %w", err)
	}
	committed = true
	return id, true, nil
}

// writeNew writes data to a file that must not exist yet (O_EXCL, which also
// refuses a symlink planted at path) and removes what it wrote on failure.
func writeNew(path string, data []byte) (retErr error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|oNoFollow, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); retErr == nil {
			retErr = cerr
		}
		if retErr != nil {
			_ = os.Remove(path)
		}
	}()
	_, err = f.Write(data)
	return err
}
