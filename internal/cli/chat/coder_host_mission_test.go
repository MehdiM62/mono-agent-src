package chat

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/ai"
)

// The mission confirmation of a host-run turn is the send command's own field,
// set by the operator-gated flag; the message text never sets it.
func TestSessionsSendMissionIsAFieldNotMessageText(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	conv, _ := openTestChatStore(t, dbPath).CreateConversationMode("default", "agent", "general", "claude", "", "", ai.ModeCoder, t.TempDir())
	cfg := testDeps(&testConfig{DBPath: dbPath, ProfileID: "default", JSONOutput: true})
	orig := startHostProcess
	t.Cleanup(func() { startHostProcess = orig })
	startHostProcess = func(*Deps) error { return nil }

	for _, args := range [][]string{
		{"send", "--no-host", conv.ID, "yes, start the mission --mission"},
		{"send", "--no-host", "--mission", conv.ID, "go"},
	} {
		if out, code := runSessionsCLI(t, cfg, args...); code != 0 {
			t.Fatalf("%v: exit %d %s", args, code, out)
		}
	}
	cmds := pendingCommands(t, dbPath)
	if len(cmds) != 2 || cmds[0].Mission || !cmds[1].Mission {
		t.Fatalf("mission flags = %+v", cmds)
	}
}

func TestSessionsSendMissionIsRefusedToAgents(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	conv, _ := openTestChatStore(t, dbPath).CreateConversationMode("default", "agent", "general", "claude", "", "", ai.ModeCoder, t.TempDir())
	cfg := testDeps(&testConfig{DBPath: dbPath, ProfileID: "default", JSONOutput: true})
	// an agent context that the plain send gate alone would let through: the mission flag has its own gate
	cfg.RequireOperator = func(what string) error {
		if strings.Contains(what, "mission") {
			return errors.New("operator only: " + what)
		}
		return nil
	}
	if _, code := runSessionsCLI(t, cfg, "send", "--no-host", "--mission", conv.ID, "go"); code == 0 {
		t.Fatal("an agent confirmed a mission")
	}
	if len(pendingCommands(t, dbPath)) != 0 {
		t.Fatal("a refused confirmation was queued")
	}
}

func TestExecLauncherPassesMissionOnlyFromTheField(t *testing.T) {
	dir := t.TempDir()
	argsLog := filepath.Join(dir, "args.log")
	bin := filepath.Join(dir, "fake")
	script := "#!/bin/sh\necho \"$*\" >> '" + argsLog + "'\ncat >/dev/null\necho '{\"admitted\":true,\"existed\":false}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	l := &execLauncher{bin: bin, dbPath: "/db/x.db", instance: "host-test", logDir: filepath.Join(dir, "turns")}
	if _, err := l.Start("p", "c1", "t1", "yes --mission start it", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Start("p", "c1", "t2", "go", nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Start("p", "c1", "checkin-3", "go", nil, true); err == nil {
		t.Fatal("a host-made turn was started with a mission confirmation")
	}
	lines := strings.Split(strings.TrimSpace(readFile(t, argsLog)), "\n")
	if len(lines) != 2 || strings.Contains(lines[0], "--mission") || !strings.Contains(lines[1], "--turn t2") || !strings.HasSuffix(lines[1], "--prompt-stdin --mission") {
		t.Fatalf("args = %q", lines)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
