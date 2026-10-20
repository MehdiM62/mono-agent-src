package profile

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/attachments"
)

const (
	// keepBackups and keepBackupsFor bound the pre-delete snapshots: one is
	// removed only when it is neither among the newest keepBackups nor
	// younger than keepBackupsFor.
	keepBackups    = 3
	keepBackupsFor = 30 * 24 * time.Hour
)

// pruneBackups removes the old pre-delete snapshots (paths oldest first, as
// ProfileDeleteBackups lists them) except current, and returns how many.
func pruneBackups(paths []string, current string, now time.Time) int {
	removed := 0
	for i, p := range paths {
		if p == current || i >= len(paths)-keepBackups {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil || now.Sub(fi.ModTime()) < keepBackupsFor {
			continue
		}
		if os.Remove(p) == nil {
			removed++
		}
	}
	return removed
}

// attachmentPaths reads the files the messages' metadata list as saved
// attachments, for the messages that satisfy where.
func attachmentPaths(ctx context.Context, db *sql.DB, where string, arg string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT metadata FROM person_messages WHERE `+where+` AND metadata LIKE '%"attachments"%'`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var raw sql.NullString
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var md struct {
			Attachments []struct {
				Path string `json:"path"`
			} `json:"attachments"`
		}
		if json.Unmarshal([]byte(raw.String), &md) != nil {
			continue
		}
		for _, a := range md.Attachments {
			if a.Path != "" {
				out[filepath.Clean(a.Path)] = true
			}
		}
	}
	return out, rows.Err()
}

// profileAttachments lists the synced attachment files only this profile's
// messages point at, and that sit in the attachments folder.
func profileAttachments(ctx context.Context, db *sql.DB, profileID string) ([]string, error) {
	mine, err := attachmentPaths(ctx, db, `profile_id = ?`, profileID)
	if err != nil {
		return nil, err
	}
	others, err := attachmentPaths(ctx, db, `profile_id <> ?`, profileID)
	if err != nil {
		return nil, err
	}
	root := filepath.Clean(attachments.Dir()) + string(filepath.Separator)
	var files []string
	for p := range mine {
		if strings.HasPrefix(p, root) && !others[p] {
			files = append(files, p)
		}
	}
	sort.Strings(files)
	return files, nil
}

// removeAttachments deletes the files, and the per-message folder that held
// them once empty. It returns how many files went and a warning per failure.
func removeAttachments(files []string) (int, []string) {
	n := 0
	var warnings []string
	for _, f := range files {
		if err := os.Remove(f); err != nil {
			if !os.IsNotExist(err) {
				warnings = append(warnings, "attachment "+f+" was not removed: "+err.Error())
			}
			continue
		}
		n++
		_ = os.Remove(filepath.Dir(f)) // only succeeds when empty
	}
	return n, warnings
}
