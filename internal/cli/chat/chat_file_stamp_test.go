package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/testdb"
)

func TestChatWorkflowStoreStampsOwnerlessWorkflowFiles(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".monoagent", "workflows")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "w-legacy.json")
	if err := os.WriteFile(path, []byte(`{"id":"w-legacy","name":"legacy","nodes":[],"connections":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.NewDatabase(testdb.Path(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if chatWorkflowStore(db, home) == nil {
		t.Fatal("no store")
	}
	b, _ := os.ReadFile(path)
	var doc struct {
		ProfileID string `json:"profile_id"`
	}
	_ = json.Unmarshal(b, &doc)
	if doc.ProfileID != "default" {
		t.Fatalf("file owner = %q, want default", doc.ProfileID)
	}
}
