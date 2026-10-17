package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// An unknown --profile reads "profile "x" not found ...", listing the profiles
// there are, whatever "open database:" a command wrapped around it; the exit
// code stays 3.
func TestUnknownProfileErrorIsNotPrefixedByTheDatabaseOpen(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE profiles (id TEXT PRIMARY KEY, name TEXT, created_at TEXT)`,
		`INSERT INTO profiles (id, name, created_at) VALUES ('p-work', 'Work', '2026-01-01')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	_, resolveErr := resolveProfileID(db, "nope")
	wrapped := fmt.Errorf("open database: %w", resolveErr)
	if exitCodeFor(wrapped) != 3 {
		t.Fatalf("exit code = %d, want 3", exitCodeFor(wrapped))
	}
	var out, errOut bytes.Buffer
	reportCommandError([]string{"workflow", "list"}, wrapped, &out, &errOut)
	got := strings.TrimSpace(errOut.String())
	if !strings.HasPrefix(got, `profile "nope" not found`) || !strings.Contains(got, "Work (p-work)") {
		t.Errorf("stderr = %q, want it to start with the profile error and list the profiles", got)
	}
}
