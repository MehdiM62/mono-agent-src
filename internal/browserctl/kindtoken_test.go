package browserctl

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// asKind gives the test the environment a spawner issues for kind
// (IssueAgentKind), and removes the record when the test ends.
func asKind(t *testing.T, kind string) {
	t.Helper()
	env, done := IssueAgentKind(kind)
	t.Cleanup(done)
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
}

func writeKindRecord(t *testing.T, token string, rec kindRecord) {
	t.Helper()
	dir, err := KindTokenDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(rec)
	if err := os.WriteFile(filepath.Join(dir, token+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// deadPID is the pid of a process that has exited.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

// IssueAgentKind writes a private record naming this process and hands
// back the environment for the agents; done removes it, and issuing again
// prunes what a crashed spawner left.
func TestIssueAgentKindWritesARecord(t *testing.T) {
	env, done := IssueAgentKind(KindOrg)
	if len(env) != 2 || env[0] != EnvAgentKind+"=org" || !strings.HasPrefix(env[1], EnvAgentKindToken+"=") {
		t.Fatalf("env: %q", env)
	}
	token := strings.TrimPrefix(env[1], EnvAgentKindToken+"=")
	dir, _ := KindTokenDir()
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("dir: %v %v", fi.Mode(), err)
	}
	path := filepath.Join(dir, token+".json")
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("record: %v %v", fi.Mode(), err)
	}
	if rec, ok := readKindRecord(path); !ok || rec.Kind != KindOrg || rec.PID != os.Getpid() || !rec.live() {
		t.Errorf("record: %+v %v", rec, ok)
	}
	done()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("done left the record: %v", err)
	}

	dead, young, old := strings.Repeat("d", 32), strings.Repeat("a", 32), strings.Repeat("b", 32)
	writeKindRecord(t, dead, kindRecord{Kind: KindOrg, PID: deadPID(t), Created: time.Now()})
	writeKindRecord(t, young, kindRecord{Kind: KindOrg, Created: time.Now().Add(-time.Hour)})
	writeKindRecord(t, old, kindRecord{Kind: KindOrg, Created: time.Now().Add(-25 * time.Hour)})
	_, done2 := IssueAgentKind(KindCoder)
	defer done2()
	for _, tok := range []string{dead, old} {
		if _, err := os.Stat(filepath.Join(dir, tok+".json")); !os.IsNotExist(err) {
			t.Errorf("record %s not pruned: %v", tok[:4], err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, young+".json")); err != nil {
		t.Errorf("a live record was pruned: %v", err)
	}
	if env, _ := IssueAgentKind(KindChat); env != nil {
		t.Errorf("chat is never declared: %q", env)
	}
}

// A declared kind counts only with a token its spawner issued: the
// variable alone, a token for another kind, a removed record, a dead
// spawner or a token that is not one all leave a shell kind other.
func TestDeclaredKindNeedsAToken(t *testing.T) {
	actAsHuman(t)
	t.Setenv(EnvAICaller, "")
	t.Setenv("CLAUDECODE", "1")
	t.Setenv(EnvAgentKind, KindOrg)
	t.Setenv(EnvAgentKindToken, "")
	if k := AgentKind(); k != KindOther {
		t.Fatalf("%s=org with no token: %q", EnvAgentKind, k)
	}
	t.Setenv(EnvAgentKind, "")
	t.Setenv("MONOMIND_ORG_ROLE", "growth")
	if k := AgentKind(); k != KindOther {
		t.Fatalf("MONOMIND_ORG_ROLE with no token: %q", k)
	}
	t.Setenv("MONOMIND_ORG_ROLE", "")

	env, done := IssueAgentKind(KindOrg)
	token := strings.TrimPrefix(env[1], EnvAgentKindToken+"=")
	t.Setenv(EnvAgentKind, KindOrg)
	t.Setenv(EnvAgentKindToken, token)
	if k := AgentKind(); k != KindOrg {
		t.Fatalf("a valid token: %q", k)
	}
	t.Setenv(EnvAgentKind, KindCoder)
	if k := AgentKind(); k != KindOther {
		t.Fatalf("an org token for coder: %q", k)
	}
	t.Setenv(EnvAgentKind, KindOrg)
	done()
	if k := AgentKind(); k != KindOther {
		t.Fatalf("the record removed: %q", k)
	}

	dead := strings.Repeat("d", 32)
	writeKindRecord(t, dead, kindRecord{Kind: KindOrg, PID: deadPID(t), Created: time.Now()})
	t.Setenv(EnvAgentKindToken, dead)
	if k := AgentKind(); k != KindOther {
		t.Fatalf("a dead spawner: %q", k)
	}
	young, old := strings.Repeat("a", 32), strings.Repeat("b", 32)
	writeKindRecord(t, young, kindRecord{Kind: KindOrg, Created: time.Now().Add(-time.Hour)})
	writeKindRecord(t, old, kindRecord{Kind: KindOrg, Created: time.Now().Add(-25 * time.Hour)})
	t.Setenv(EnvAgentKindToken, young)
	if k := AgentKind(); k != KindOrg {
		t.Fatalf("no pid, an hour old: %q", k)
	}
	t.Setenv(EnvAgentKindToken, old)
	if k := AgentKind(); k != KindOther {
		t.Fatalf("no pid, a day old: %q", k)
	}
	for _, bad := range []string{"../" + young, strings.ToUpper(young), young + "x"} {
		t.Setenv(EnvAgentKindToken, bad)
		if k := AgentKind(); k != KindOther {
			t.Fatalf("token %q: %q", bad, k)
		}
	}
}
