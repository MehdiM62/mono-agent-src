package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// data/migrations.txt lists every migration, oldest first, a new one added as
// its last line. CI cannot see two open branches that each take the next
// migration number (TestEmbeddedMigrationVersionsUnique only fails once both
// are on master); the manifest makes them meet: both branches change its last
// line, so the second to merge has a conflict to resolve first.
func TestMigrationManifestListsEveryMigration(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "data", "migrations.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			listed = append(listed, line)
		}
	}
	all, err := embeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, m := range all {
		files = append(files, m.filename)
	}
	if strings.Join(listed, "\n") == strings.Join(files, "\n") {
		return
	}
	inManifest := map[string]bool{}
	for _, f := range listed {
		inManifest[f] = true
	}
	for _, f := range files {
		if !inManifest[f] {
			t.Errorf("%s is missing from data/migrations.txt: add it as the last line, in the change that adds the migration", f)
		}
		delete(inManifest, f)
	}
	for f := range inManifest {
		t.Errorf("data/migrations.txt lists %s, which is not in data/migrations", f)
	}
	if !t.Failed() {
		t.Errorf("data/migrations.txt is not in migration order: each migration must come after every older one\n got: %s\nwant: %s",
			strings.Join(listed, " "), strings.Join(files, " "))
	}
}
