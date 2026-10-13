package chat

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Destructive-op snapshots — every delete (and field-overwriting update)
// below writes a sidecar backup first and FAILS CLOSED if the backup
// cannot be written: an irreversible operation must never proceed
// unsnapshotted. Mirrors the canvas delete_nodes backup pattern.
// ---------------------------------------------------------------------------

// monoToolBackupDir resolves ~/.monoagent/ai-tool-backups. Package var so
// tests can redirect it.
var monoToolBackupDir = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".monoagent", "ai-tool-backups"), nil
}

// maxMonoToolBackups bounds the rotation: the newest
// maxMonoToolBackups sidecars are kept per <kind>-<id>.
const maxMonoToolBackups = 20

// snapshotRows materializes a query as generic rows for backup storage.
// []byte values stay []byte so BLOB columns (e.g. vault ciphertext)
// round-trip losslessly (JSON base64); TEXT columns arrive as strings.
func snapshotRows(db *sql.DB, query string, args ...interface{}) ([]map[string]interface{}, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	for rows.Next() {
		raw := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]interface{}, len(cols))
		for i, c := range cols {
			m[c] = raw[i]
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// sanitizeBackupID keeps ids filesystem-safe inside backup filenames.
func sanitizeBackupID(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// saveMonoToolBackup writes the pre-destruction snapshot of kind/id to
// <dir>/<kind>-<id>-<ts>.json (0600) and rotates the per-kind+id history
// down to maxMonoToolBackups. Returns the sidecar path.
func saveMonoToolBackup(kind, id, operation string, tables map[string][]map[string]interface{}) (string, error) {
	dir, err := monoToolBackupDir()
	if err != nil {
		return "", fmt.Errorf("resolve backup dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	now := time.Now().UTC()
	envelope := map[string]interface{}{
		"kind":       kind,
		"id":         id,
		"operation":  operation,
		"created_at": now.Format(time.RFC3339Nano),
		"tables":     tables,
	}
	b, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal backup: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s-%s.json", kind, sanitizeBackupID(id), now.Format("20060102T150405.000000000Z07:00")))
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}
	pruneMonoToolBackups(dir, fmt.Sprintf("%s-%s-", kind, sanitizeBackupID(id)))
	return path, nil
}

// pruneMonoToolBackups keeps only the newest maxMonoToolBackups files
// sharing prefix. Best-effort by design: a prune failure never fails the
// already-snapshotted destructive op.
func pruneMonoToolBackups(dir, prefix string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	if len(names) <= maxMonoToolBackups {
		return
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names))) // timestamped names sort chronologically
	for _, name := range names[maxMonoToolBackups:] {
		_ = os.Remove(filepath.Join(dir, name))
	}
}
