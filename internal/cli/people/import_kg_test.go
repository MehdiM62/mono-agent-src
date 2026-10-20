package people

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// people import syncs the people it saved to the knowledge graph, and the
// command waits for that sync before it exits.
func TestPeopleImportSyncsToKnowledgeGraph(t *testing.T) {
	cfg, _ := newReviewCLITestDB(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "synced")
	bin := filepath.Join(dir, "monomind")
	script := "#!/bin/sh\nsleep 0.3\nprintf '%s' \"$*\" > " + marker + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MONOMIND_BIN", bin)
	file := filepath.Join(dir, "p.json")
	if err := os.WriteFile(file, []byte(`[{"username":"kg-ann","full_name":"Ann Kg"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runPeople(t, cfg, "import", "--file", file, "--platform", "x"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("import returned before its knowledge-graph sync ran: %v", err)
	}
	if !strings.Contains(string(got), "Ann Kg") {
		t.Fatalf("sync did not carry the person: %s", got)
	}
}
