// internal/vault/document_exclusions.go
package vault

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExcludedDocument is a profile-folder file the user removed from MonoAgent:
// the folder scan skips it until it is restored.
type ExcludedDocument struct {
	Path      string
	Filename  string
	CreatedAt string
}

// ExcludeDocument records that path (absolute, as vault_documents holds it)
// must not be registered again by the folder scan. Idempotent.
func ExcludeDocument(ctx context.Context, db *sql.DB, profileID, path, filename string) error {
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO vault_document_exclusions (profile_id, path, filename) VALUES (?, ?, ?)`,
		profileID, path, filename); err != nil {
		return fmt.Errorf("vault.ExcludeDocument: %w", err)
	}
	return nil
}

// ListExcludedDocuments returns profileID's exclusions, newest first.
func ListExcludedDocuments(ctx context.Context, db *sql.DB, profileID string) ([]ExcludedDocument, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT path, filename, created_at FROM vault_document_exclusions WHERE profile_id = ? ORDER BY created_at DESC, path`, profileID)
	if err != nil {
		return nil, fmt.Errorf("vault.ListExcludedDocuments: %w", err)
	}
	defer rows.Close()
	out := []ExcludedDocument{}
	for rows.Next() {
		var e ExcludedDocument
		if err := rows.Scan(&e.Path, &e.Filename, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("vault.ListExcludedDocuments: scan: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// RestoreExcludedDocument lifts the exclusion on path, so the next folder
// scan registers the file again. It reports whether there was one.
func RestoreExcludedDocument(ctx context.Context, db *sql.DB, profileID, path string) (bool, error) {
	res, err := db.ExecContext(ctx, "DELETE FROM vault_document_exclusions WHERE profile_id = ? AND path = ?", profileID, path)
	if err != nil {
		return false, fmt.Errorf("vault.RestoreExcludedDocument: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// excludedPaths is the set of paths excluded for profileID.
func excludedPaths(ctx context.Context, db *sql.DB, profileID string) (map[string]bool, error) {
	list, err := ListExcludedDocuments(ctx, db, profileID)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(list))
	for _, e := range list {
		set[e.Path] = true
	}
	return set, nil
}

// DocumentIDByPath returns the id of profileID's document at path, if any.
func DocumentIDByPath(ctx context.Context, db *sql.DB, profileID, path string) (string, bool, error) {
	return documentIDByPath(ctx, db, profileID, path)
}

// RemoveDiscoveredFile deletes the file of a discovered document from the
// profile folder root, if CheckDiscoveredFile accepts it.
func RemoveDiscoveredFile(root, path string) error {
	resolved, err := discoveredFilePath(root, path)
	if err != nil {
		return err
	}
	return os.Remove(resolved)
}

// CheckDiscoveredFile reports why path may not be deleted as a file of the
// profile folder root, nil when it may.
func CheckDiscoveredFile(root, path string) error {
	_, err := discoveredFilePath(root, path)
	return err
}

// discoveredFilePath returns path with symlinked directories resolved. It
// refuses anything but a regular file that lies, after resolving symlinks,
// inside root: no path outside the folder, no ".." escape, no symlinked file
// or directory leading out, no directory.
func discoveredFilePath(root, path string) (string, error) {
	if root == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("refusing to delete %q: not an absolute path inside the profile folder", path)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolving the profile folder: %w", err)
	}
	// Resolve the directory, not the file: a symlinked file is refused below
	// by Lstat rather than followed to wherever it points.
	realDir, err := filepath.EvalSymlinks(filepath.Dir(filepath.Clean(path)))
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", path, err)
	}
	resolved := filepath.Join(realDir, filepath.Base(path))
	rel, err := filepath.Rel(realRoot, resolved)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("refusing to delete %q: it is outside the profile folder", path)
	}
	fi, err := os.Lstat(resolved)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("refusing to delete %q: not a regular file", path)
	}
	return resolved, nil
}
