package chat

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/ai"
	"github.com/monoes/mono-agent/internal/coderhost"
	"github.com/monoes/mono-agent/internal/storage"
)

// masterTurn is a master session in the test database that is running a turn, with its token.
type masterTurn struct {
	dbPath, master, token string
	cfg                   *Deps
}

func newMasterTurn(t *testing.T) masterTurn {
	t.Helper()
	dbPath := newChatCLITestDB(t)
	store := openTestChatStore(t, dbPath)
	conv, err := store.CreateConversationAccess("default", "agent", "general", "claude", "", "", ai.AccessWorkspace, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cs := coderhost.NewStore(db.DB)
	if err := cs.SetRole("default", conv.ID, "master", ""); err != nil {
		t.Fatal(err)
	}
	x, _, _ := cs.GetSession("default", conv.ID)
	x.State, x.TurnID = coderhost.StateRunning, "turn-1"
	if err := cs.SaveSession(x); err != nil {
		t.Fatal(err)
	}
	tok, err := cs.IssueSessionToken("default", conv.ID, "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	// an agent: the operator path is closed, and the host must never be started from here
	cfg := testDeps(&testConfig{DBPath: dbPath, ProfileID: "default", JSONOutput: true})
	cfg.RequireOperator = func(what string) error { return errors.New("operator only: " + what) }
	orig := startHostProcess
	t.Cleanup(func() { startHostProcess = orig })
	startHostProcess = func(*Deps) error { t.Error("a master's verb started the host"); return nil }
	t.Setenv(coderhost.SessionTokenEnv, tok)
	return masterTurn{dbPath: dbPath, master: conv.ID, token: tok, cfg: cfg}
}

func TestMasterVerbsSpawnAndControlOnlyDescendantsWithTheSessionToken(t *testing.T) {
	m := newMasterTurn(t)
	var spawned struct{ ConversationID string }
	out, code := runSessionsCLI(t, m.cfg, "spawn", "--message", "write tests")
	if code != 0 {
		t.Fatalf("spawn: exit %d %s", code, out)
	}
	decodeChatJSON(t, out, &spawned)
	kid := spawned.ConversationID
	if _, code := runSessionsCLI(t, m.cfg, "spawn", "--access", "full"); code == 0 {
		t.Fatal("a full-access child was spawned")
	}
	for _, args := range [][]string{{"send", kid, "next"}, {"pause", kid}, {"resume", kid}, {"stop", kid}, {"settings", "set", kid, "--patch", `{"checkin_minutes":5}`}} {
		if out, code := runSessionsCLI(t, m.cfg, args...); code != 0 {
			t.Errorf("%v: exit %d %s", args, code, out)
		}
	}
	cmds := pendingCommands(t, m.dbPath)
	sends := 0
	for _, c := range cmds {
		if c.Kind == coderhost.KindSend {
			sends++
			if !strings.HasPrefix(c.TurnID, coderhost.MasterTurnPrefix) || c.WantsMission {
				t.Errorf("send = %+v", c)
			}
		}
	}
	if sends != 2 { // the spawn's first message and the send
		t.Fatalf("queued = %+v", cmds)
	}

	before := len(pendingCommands(t, m.dbPath))
	for _, args := range [][]string{
		{"send", m.master, "to myself"}, {"stop", m.master}, {"pause", "no-such"}, // itself, nothing
		{"send", kid, "x", "--mission"}, {"send", kid, "x", "--attach", "/etc/passwd"}, // a master's send is no mission
		{"settings", "set", kid, "--patch", `{"max_children":9}`}, {"settings", "set", kid, "--patch", `{"mesh_accept":"project"}`}, // operator-only settings
		{"settings", "set", kid, "--role", "solo"}, {"settings", "set", kid, "--unattended", "false"}, {"settings", "set", kid, "--budget-usd", "5"},
	} {
		if out, code := runSessionsCLI(t, m.cfg, args...); code == 0 {
			t.Errorf("%v was allowed: %s", args, out)
		}
	}
	if len(pendingCommands(t, m.dbPath)) != before {
		t.Fatal("a refused verb queued a command")
	}
	if out, code := runSessionsCLI(t, m.cfg, "status"); code != 0 || !strings.Contains(out, kid) {
		t.Fatalf("status: %d %s", code, out)
	}
}

func TestMasterVerbsRefuseAForgedOrStaleTokenAndNeverFallBackToTheOperator(t *testing.T) {
	m := newMasterTurn(t)
	kid := func() string {
		var s struct{ ConversationID string }
		out, code := runSessionsCLI(t, m.cfg, "spawn")
		if code != 0 {
			t.Fatal(out)
		}
		decodeChatJSON(t, out, &s)
		return s.ConversationID
	}()
	before := len(pendingCommands(t, m.dbPath))
	operator := testDeps(&testConfig{DBPath: m.dbPath, ProfileID: "default", JSONOutput: true}) // an operator who also holds a bad token
	for _, tok := range []string{"mst-forged", m.token + "x", "garbage"} {
		t.Setenv(coderhost.SessionTokenEnv, tok)
		for _, cfg := range []*Deps{m.cfg, operator} {
			for _, args := range [][]string{{"send", kid, "x"}, {"pause", kid}, {"spawn"}, {"status"}, {"settings", "set", kid, "--patch", `{"checkin_minutes":5}`}} {
				if _, code := runSessionsCLI(t, cfg, args...); code == 0 {
					t.Errorf("%v with token %q was allowed", args, tok)
				}
			}
		}
	}
	// the token of another profile's session does not verify under this profile
	t.Setenv(coderhost.SessionTokenEnv, m.token)
	other := testDeps(&testConfig{DBPath: m.dbPath, ProfileID: "other", JSONOutput: true})
	if _, code := runSessionsCLI(t, other, "spawn"); code == 0 {
		t.Error("a token worked under another profile")
	}
	if len(pendingCommands(t, m.dbPath)) != before {
		t.Fatal("a refused verb queued a command")
	}
	// without any token spawn is closed, and the old operator-only verbs stay closed for an agent
	t.Setenv(coderhost.SessionTokenEnv, "")
	for _, args := range [][]string{{"spawn"}, {"status"}, {"send", kid, "x"}, {"pause", kid}} {
		if _, code := runSessionsCLI(t, m.cfg, args...); code == 0 {
			t.Errorf("%v without a token was allowed for an agent", args)
		}
	}
}

// The turn's environment carries its own session token and nothing a session that started the host held.
func TestExecLauncherGivesATurnItsOwnSessionTokenOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	dir := t.TempDir()
	envLog := filepath.Join(dir, "env.txt")
	bin := filepath.Join(dir, "fake-monoagentcli")
	script := "#!/bin/sh\ncat > /dev/null\nenv | grep MONOAGENT_ | sort > '" + envLog + "'\necho '{\"admitted\":true,\"existed\":false,\"turn\":{\"id\":\"t1\"}}'\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(coderhost.SessionTokenEnv, "mst-the-token-of-the-session-that-started-the-host")
	var asked []string
	revoked := 0
	l := &execLauncher{bin: bin, dbPath: "/db/x.db", instance: "host-test", logDir: filepath.Join(dir, "turns"),
		tokens: func(p, c, turn string) (string, error) {
			asked = append(asked, p+"/"+c+"/"+turn)
			return "mst-own", nil
		},
		revoke: func(p, c string) { revoked++ }}
	turn, err := l.Start("p", "c1", "t1", "hi", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = turn.Wait()
	got, _ := os.ReadFile(envLog)
	if !strings.Contains(string(got), coderhost.SessionTokenEnv+"=mst-own\n") || strings.Contains(string(got), "the-session-that-started-the-host") {
		t.Fatalf("env = %q", got)
	}
	if len(asked) != 1 || asked[0] != "p/c1/t1" || revoked != 1 {
		t.Fatalf("asked %v, revoked %d", asked, revoked)
	}
	// no token minter: still none of the host's inherited one
	l2 := &execLauncher{bin: bin, dbPath: "/db/x.db", instance: "host-test", logDir: filepath.Join(dir, "turns2")}
	turn2, err := l2.Start("p", "c1", "t2", "hi", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = turn2.Wait()
	if got, _ := os.ReadFile(envLog); strings.Contains(string(got), coderhost.SessionTokenEnv) {
		t.Fatalf("a turn inherited a session token: %q", got)
	}
}
