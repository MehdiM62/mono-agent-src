package profile

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/storage"
)

// A name shared by two profiles (case differs) is refused by every positional
// command, as the scoped commands refuse it, instead of picking one.
func TestProfilePositionalNameAmbiguityIsAnError(t *testing.T) {
	dbPath := newProfileDocsCLITestDB(t)
	store, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range [][2]string{{"id-a", "Gtmp"}, {"id-b", "gtmp"}} {
		if _, err := store.DB.Exec(`INSERT INTO profiles (id, name, created_at) VALUES (?, ?, '2026-01-01')`, r[0], r[1]); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()

	for _, args := range [][]string{{"get", "gtmp"}, {"folder", "gtmp"}, {"move", "gtmp", t.TempDir()}} {
		_, err := runProfileJSON(t, dbPath, args...)
		if err == nil || exitCodeOf(t, err) != 3 || !strings.Contains(err.Error(), "id-a") || !strings.Contains(err.Error(), "id-b") {
			t.Errorf("profile %v = %v, want the ambiguous error (exit 3) listing both ids", args, err)
		}
	}
	var got profileJSON
	mustProfileJSON(t, dbPath, &got, "get", "id-b")
	if got.ID != "id-b" {
		t.Fatalf("get by id = %+v", got)
	}
}
