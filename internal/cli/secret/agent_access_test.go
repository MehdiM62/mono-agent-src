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
	out, err = runSecretCmd(t, db, "agent-access", "on", "--yes", "--any-host")
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
	if _, err := runSecretCmd(t, db, "agent-access", "on", "--yes", "--any-host"); err != nil {
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
	if _, err := runSecretCmd(t, db, "agent-access", "on", "--yes", "--any-host"); err == nil {
		t.Fatal("an agent turned it on")
	}
	out, err := runSecretCmd(t, db, "agent-access")
	if err != nil || !strings.Contains(out, `"agent_access": false`) {
		t.Fatalf("an agent reading it: %v %s", err, out)
	}
}

// The CLI resolves --profile NAME to the profile's id only once the database
// is open: the command must read it then, not before (the id is what the
// table's foreign key names).
func TestAgentAccessUsesTheProfileIDResolvedByOpeningTheDatabase(t *testing.T) {
	db := newSecretCLITestDB(t)
	seed, err := storage.NewDatabase(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.DB.Exec(`INSERT INTO profiles (id, name) VALUES ('81ecb86c-id', 'monoes')`); err != nil {
		t.Fatal(err)
	}
	seed.DB.Close()
	d := testDeps(db, "", true)
	open := d.OpenDB
	resolved := false
	d.OpenDB = func() (*storage.Database, error) { resolved = true; return open() }
	d.ProfileID = func() string {
		if resolved {
			return "81ecb86c-id"
		}
		return "monoes"
	}
	cmd := NewCommand(d)
	cmd.SetArgs([]string{"agent-access", "on", "--yes", "--any-host"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("on: %v", err)
	}
	out, err := runSecretCmdWith(d, "agent-access")
	if err != nil || !strings.Contains(out, `"agent_access": true`) || !strings.Contains(out, "81ecb86c-id") {
		t.Fatalf("status: %v %s", err, out)
	}
}

func runSecretCmdWith(d Deps, args ...string) (string, error) {
	cmd := NewCommand(d)
	cmd.SetArgs(args)
	var out strings.Builder
	cmd.SetOut(&out)
	err := cmd.Execute()
	return out.String(), err
}

// Turning it on says where a secret may go: --host, or --any-host on purpose.
func TestAgentAccessOnNeedsAHostOrAnyHostAndKeepsItsScope(t *testing.T) {
	db := newSecretCLITestDB(t)
	profileP1(t, db)
	if _, err := runSecretCmd(t, db, "agent-access", "on", "--yes"); err == nil || !strings.Contains(err.Error(), "--any-host") {
		t.Fatalf("on with neither --host nor --any-host: %v", err)
	}
	if _, err := runSecretCmd(t, db, "agent-access", "on", "--yes", "--host", "a.example", "--any-host"); err == nil {
		t.Fatal("--host with --any-host")
	}
	if _, err := runSecretCmd(t, db, "agent-access", "on", "--yes", "--any-host", "--expires", "soon"); err == nil {
		t.Fatal("a bad --expires")
	}
	if out, _ := runSecretCmd(t, db, "agent-access"); !strings.Contains(out, `"agent_access": false`) {
		t.Fatalf("a refused on turned it on: %s", out)
	}
	out, err := runSecretCmd(t, db, "agent-access", "on", "--yes", "--host", "API.Example.com,https://hooks.example.org/x", "--secret", "a,b", "--expires", "24h")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"agent_access": true`, `"api.example.com"`, `"hooks.example.org"`, `"a"`, `"b"`, `"any_host": false`, `"expires_at": "`} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %s: %s", want, out)
		}
	}
}

func TestAgentAccessAuditListsNamesAndOnlyTheUserClearsIt(t *testing.T) {
	dbPath := newSecretCLITestDB(t)
	profileP1(t, dbPath)
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO profile_agent_secret_audit (profile_id, workflow_id, execution_id, node_id, node_name, secret_name) VALUES ('p1', 'wf-1', 'ex-1', 'n1', 'Send', 'github')`); err != nil {
		t.Fatal(err)
	}
	db.DB.Close()
	out, err := runSecretCmd(t, dbPath, "agent-access", "audit")
	if err != nil || !strings.Contains(out, `"secret": "github"`) || !strings.Contains(out, `"workflow_id": "wf-1"`) || !strings.Contains(out, `"execution_id": "ex-1"`) {
		t.Fatalf("audit: %v %s", err, out)
	}
	t.Setenv("CLAUDECODE", "1")
	if _, err := runSecretCmd(t, dbPath, "agent-access", "audit", "--clear"); err == nil || !strings.Contains(err.Error(), "CLAUDECODE is set") {
		t.Fatalf("an agent cleared the audit: %v", err)
	}
	if out, err := runSecretCmd(t, dbPath, "agent-access", "audit"); err != nil || !strings.Contains(out, "github") {
		t.Fatalf("an agent may read it: %v %s", err, out)
	}
	t.Setenv("CLAUDECODE", "")
	if _, err := runSecretCmd(t, dbPath, "agent-access", "audit", "--clear"); err != nil {
		t.Fatal(err)
	}
	if out, _ := runSecretCmd(t, dbPath, "agent-access", "audit"); strings.Contains(out, "github") {
		t.Fatalf("not cleared: %s", out)
	}
}
