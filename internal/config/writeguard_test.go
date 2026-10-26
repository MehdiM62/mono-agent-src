package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/testdb"
)

// A raw storage write of a reserved row is refused unless the caller is the
// operator; any other row, and the operator, pass. The refused write leaves
// the row as it was.
func TestStorageSaveConfigGuardsReservedRows(t *testing.T) {
	var marker string
	old := storage.ConfigWriteGuard
	storage.ConfigWriteGuard = newConfigWriteGuard(func() string { return marker })
	t.Cleanup(func() { storage.ConfigWriteGuard = old })

	db := testdb.Open(t)
	if err := db.SaveConfig("memory.runs", "on"); err != nil { // the operator
		t.Fatalf("operator write: %v", err)
	}
	marker = "an agent marker"
	for _, name := range []string{"memory.runs", "workflow.error_workflow", " Coder_Mode", "coder-mission-confirm/x/y"} {
		if err := db.SaveConfig(name, "off"); err == nil || !strings.Contains(err.Error(), "reserved setting") {
			t.Errorf("%q by a non-operator: %v", name, err)
		}
	}
	if e, _ := db.GetConfig("memory.runs"); e == nil || e.ConfigData != "on" {
		t.Errorf("memory.runs after refused writes: %+v", e)
	}
	if e, _ := db.GetConfig("coder_mode"); e != nil {
		t.Errorf("coder_mode was written: %+v", e)
	}
	if err := db.SaveConfig("linkedin_login_x", "{}"); err != nil {
		t.Errorf("an ordinary row: %v", err)
	}
}

// The package installs the guard itself, so every binary that links the
// config store has it; it uses the real operator check.
func TestStorageGuardIsInstalledAtStartup(t *testing.T) {
	if storage.ConfigWriteGuard == nil {
		t.Fatal("storage.ConfigWriteGuard is not installed")
	}
	t.Setenv("MONOAGENT_WORKFLOW_RUN", "wf-1")
	db := testdb.Open(t)
	if err := db.SaveConfig("memory.runs", "off"); err == nil {
		t.Error("a workflow run's raw write of a reserved row passed")
	}
}

// writeAllowlist names every non-test file that writes the configs table
// (Database.SaveConfig or SQL on it), with why it may. A new writer has to be
// added here on purpose: it must either go through the operator check or only
// ever write rows no agent-reachable command can name.
var writeAllowlist = map[string]string{
	"internal/storage/repository_config.go":    "Database.SaveConfig itself: consults ConfigWriteGuard",
	"internal/config/dbstore.go":               "DBConfigStore: refuses every reserved name before it saves",
	"internal/config/manager.go":               "generated selector configs: refuses a reserved name, then saves through the store",
	"internal/cli/configcli/config.go":         "config set/delete: guardReserved asks the operator check first",
	"cmd/monoagentcli/sandbox.go":              "sandbox fallback --set: operator-only command",
	"internal/browserctl/permissions.go":       "browser permissions/site: operator-only commands",
	"internal/cli/chat/coder.go":               "coder enable/set: operator-only flags",
	"internal/cli/chat/coder_projects.go":      "coder projects map/unmap: operator-only",
	"internal/coderhost/missionconfirm.go":     "the coder host's signed per-send confirmations, written for the operator's own confirm",
	"internal/ai/chat_events_conversations.go": "revokes (deletes) confirmations when a conversation is deleted; never writes a value",
}

var configsWrite = regexp.MustCompile("(?i)(\\bsaveconfig\\(|\\bdeleteconfig\\(|insert\\s+(or\\s+\\w+\\s+)?into\\s+configs\\b|update\\s+configs\\b|delete\\s+from\\s+configs\\b)")

// No file outside writeAllowlist writes the configs table.
func TestConfigsTableWritersAreAllowlisted(t *testing.T) {
	root := filepath.Join("..", "..")
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "frontend", "chrome-extension":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		// some sources split a keyword across a concatenation
		src := strings.NewReplacer("`+`", "", "\"+\"", "").Replace(string(b))
		if !configsWrite.MatchString(src) {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		seen[rel] = true
		if _, ok := writeAllowlist[rel]; !ok {
			t.Errorf("%s writes the configs table and is not in writeAllowlist: route it through the operator check or justify it there", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel := range writeAllowlist {
		if !seen[rel] {
			t.Errorf("writeAllowlist names %s, which no longer writes the configs table: drop it", rel)
		}
	}
}
