package secret

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/storage"
)

// profileP1 gives the test database the profile the helpers run in.
func profileP1(t *testing.T, dbPath string) {
	t.Helper()
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	if _, err := db.DB.Exec(`INSERT OR IGNORE INTO profiles (id, name) VALUES ('p1', 'p1')`); err != nil {
		t.Fatal(err)
	}
}

// Agent access to the vault is off by default and is the user's own to turn
// on: an agent, the chat or a workflow cannot, and nothing changes when it tries.

func TestAgentAccessIsOffUntilTheUserTurnsItOn(t *testing.T) {
	db := newSecretCLITestDB(t)
	profileP1(t, db)
	out, err := runSecretCmd(t, db, "agent-access")
	if err != nil || !strings.Contains(out, `"agent_access": false`) {
		t.Fatalf("default: %v %s", err, out)
	}
	if _, err := runSecretCmd(t, db, "agent-access", "on"); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("on without --yes: %v", err)
	}
	if out, _ := runSecretCmd(t, db, "agent-access"); !strings.Contains(out, `"agent_access": false`) {
		t.Fatalf("still off: %s", out)
	}
	out, err = runSecretCmd(t, db, "agent-access", "on", "--yes")
	if err != nil || !strings.Contains(out, `"agent_access": true`) {
		t.Fatalf("on: %v %s", err, out)
	}
	out, err = runSecretCmd(t, db, "agent-access", "off")
	if err != nil || !strings.Contains(out, `"agent_access": false`) {
		t.Fatalf("off: %v %s", err, out)
	}
}

func TestAgentAccessCannotBeChangedByAnAgent(t *testing.T) {
	db := newSecretCLITestDB(t)
	profileP1(t, db)
	if _, err := runSecretCmd(t, db, "agent-access", "on", "--yes"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDECODE", "1")
	_, err := runSecretCmd(t, db, "agent-access", "off")
	if err == nil || !strings.Contains(err.Error(), "CLAUDECODE is set") {
		t.Fatalf("an agent turned it off: %v", err)
	}
	// Nor can it turn it on from off, and it may still read the state.
	t.Setenv("CLAUDECODE", "")
	if _, err := runSecretCmd(t, db, "agent-access", "off"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDECODE", "1")
	if _, err := runSecretCmd(t, db, "agent-access", "on", "--yes"); err == nil {
		t.Fatal("an agent turned it on")
	}
	out, err := runSecretCmd(t, db, "agent-access")
	if err != nil || !strings.Contains(out, `"agent_access": false`) {
		t.Fatalf("an agent reading it: %v %s", err, out)
	}
}
