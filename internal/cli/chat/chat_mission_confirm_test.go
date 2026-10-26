package chat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/ai"
	"github.com/monoes/mono-agent/internal/coderhost"
	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/tasks"
)

func confirmStore(t *testing.T, dbPath string) (*coderhost.Store, func()) {
	t.Helper()
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	return coderhost.NewStore(db.DB), func() { db.Close() }
}

func sendCoderDeps(t *testing.T) (string, string, *Deps) {
	t.Helper()
	dbPath := newChatCLITestDB(t)
	conv, _ := openTestChatStore(t, dbPath).CreateConversationMode("default", "agent", "general", "claude", "", "", ai.ModeCoder, t.TempDir())
	return dbPath, conv.ID, testDeps(&testConfig{DBPath: dbPath, ProfileID: "default", JSONOutput: true})
}

// The confirmation is a record only the operator-gated send --mission writes:
// the command row carries none of it, and neither does the message's text.
func TestSessionsSendMissionWritesTheOperatorsRecord(t *testing.T) {
	dbPath, conv, cfg := sendCoderDeps(t)
	run := func(args ...string) {
		t.Helper()
		if out, code := runSessionsCLI(t, cfg, append([]string{"send", "--no-host"}, args...)...); code != 0 {
			t.Fatalf("%v: exit %d %s", args, code, out)
		}
	}
	run("--turn", "t-plain", conv, "yes, start the mission --mission")
	run("--turn", "t-yes", "--mission", conv, "go")
	cs, closeDB := confirmStore(t, dbPath)
	defer closeDB()
	if _, ok := cs.PeekConfirm("default", conv, "t-plain"); ok {
		t.Fatal("message text confirmed a mission")
	}
	c, ok := cs.PeekConfirm("default", conv, "t-yes")
	if !ok || c.Session != 0 || c.TurnID != "t-yes" {
		t.Fatalf("confirmation = %+v ok=%v", c, ok)
	}
	for _, cmd := range pendingCommands(t, dbPath) {
		if strings.Contains(cmd.Message+strings.Join(cmd.Attachments, ","), c.Nonce) {
			t.Fatalf("the command row carries the confirmation: %+v", cmd)
		}
	}
}

func TestSessionsSendMissionIsRefusedToAgentsAndHostTurns(t *testing.T) {
	dbPath, conv, cfg := sendCoderDeps(t)
	// an agent context the plain send gate alone lets through: the flag has its own gate
	agent := testDeps(&testConfig{DBPath: dbPath, ProfileID: "default", JSONOutput: true})
	agent.RequireOperator = func(what string) error {
		if strings.Contains(what, "mission") {
			return errors.New("operator only: " + what)
		}
		return nil
	}
	cs, closeDB := confirmStore(t, dbPath)
	defer closeDB()
	if _, code := runSessionsCLI(t, agent, "send", "--no-host", "--turn", "t-a", "--mission", conv, "go"); code == 0 {
		t.Fatal("an agent confirmed a mission")
	}
	if _, ok := cs.PeekConfirm("default", conv, "t-a"); ok || len(pendingCommands(t, dbPath)) != 0 {
		t.Fatal("a refused confirmation left a record or a command")
	}
	if _, code := runSessionsCLI(t, cfg, "send", "--no-host", "--turn", "checkin-x", "--mission", conv, "go"); code == 0 {
		t.Fatal("a host-made turn id was confirmed")
	}
	if _, ok := cs.PeekConfirm("default", conv, "checkin-x"); ok {
		t.Fatal("a check-in turn carries a confirmation")
	}
}

// The child consumes the record: once, for the mission state it was given in.
func TestHostMissionConfirmedIsSingleUseAndBoundToTheSession(t *testing.T) {
	dbPath, conv, cfg := sendCoderDeps(t)
	cs, closeDB := confirmStore(t, dbPath)
	defer closeDB()
	c, err := cs.Confirm("default", conv, "t1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if hostMissionConfirmed(cfg, "default", conv, "t1", "wrong-nonce", "coding") || hostMissionConfirmed(cfg, "default", conv, "t2", c.Nonce, "coding") ||
		hostMissionConfirmed(cfg, "default", conv, "t1", c.Nonce, "scoped") || hostMissionConfirmed(cfg, "default", conv, "t1", "", "coding") {
		t.Fatal("a wrong nonce, turn, access or no token confirmed")
	}
	if !hostMissionConfirmed(cfg, "default", conv, "t1", c.Nonce, "coding") {
		t.Fatal("the operator's confirmation was refused")
	}
	if hostMissionConfirmed(cfg, "default", conv, "t1", c.Nonce, "coding") {
		t.Fatal("a consumed confirmation confirmed again (restart / re-sent turn)")
	}
	// a confirmation made while the lead was only proposing does not carry over to a mission session that exists now
	c2, _ := cs.Confirm("default", conv, "t3", 0)
	db, _ := storage.NewDatabase(dbPath)
	defer db.Close()
	if _, err := tasks.NewStore(db.DB).CreateSession(context.Background(), "default", tasks.SessionInput{Title: "M", ConversationID: conv}, tasks.Actor{Kind: tasks.Human}); err != nil {
		t.Fatal(err)
	}
	if hostMissionConfirmed(cfg, "default", conv, "t3", c2.Nonce, "coding") {
		t.Fatal("a confirmation for one mission state confirmed another")
	}
}

func TestExecLauncherPassesTheTokenNeverABareMissionFlag(t *testing.T) {
	dir := t.TempDir()
	argsLog := filepath.Join(dir, "args.log")
	bin := filepath.Join(dir, "fake")
	script := "#!/bin/sh\necho \"$*\" >> '" + argsLog + "'\ncat >/dev/null\necho '{\"admitted\":true,\"existed\":false}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	l := &execLauncher{bin: bin, dbPath: "/db/x.db", instance: "host-test", logDir: filepath.Join(dir, "turns")}
	if _, err := l.Start("p", "c1", "t1", "yes --mission start it", nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Start("p", "c1", "t2", "go", nil, "abc123"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Start("p", "c1", "t3", "go", nil, "--mission"); err == nil {
		t.Fatal("a token that reads as a flag was accepted")
	}
	b, _ := os.ReadFile(argsLog)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || strings.Contains(lines[0], "--mission") || !strings.HasSuffix(lines[1], "--prompt-stdin --mission-token=abc123") || strings.Contains(lines[1], " --mission ") {
		t.Fatalf("args = %q", lines)
	}
}

// End to end through the real chat command: the token the host passes turns the
// planning tools on for that one turn; a bare token, a used one, or none does not.
func TestHostRunTurnGetsMissionToolsOnlyFromTheOperatorsRecord(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	dbPath := newChatCLITestDB(t)
	bin, argsLog := writeOrgMonomind(t)
	withCoderCaps(t, append(monomind.CoderCapabilities, monomind.CapAgentExecFullAccessTools, monomind.CapAgentExecAccessRead)...)
	withCoderScan(t, orgScan(true))
	setCoderSettings(t, dbPath, coderSettings{Enabled: true})
	conv, err := openTestChatStore(t, dbPath).CreateConversationMode("default", "agent", "general", "claude", "", "opus", ai.ModeCoder, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, code := runChatHistory(t, dbPath, "default", "set-org", conv.ID, "dynamic"); code != 0 {
		t.Fatalf("set-org: exit %d", code)
	}
	turn := func(id string, extra ...string) string {
		t.Helper()
		args := append([]string{"--conversation", conv.ID, "--turn", id}, extra...)
		if out, err := runChatCmd(t, dbPath, bin, append(args, "--", "start the mission --mission")...); err != nil {
			t.Fatalf("turn %s: %v\n%s", id, err, out)
		}
		return leadToolNames(t, argsLog)
	}
	cs, closeDB := confirmStore(t, dbPath)
	defer closeDB()
	const proposeOnly, planning = "org_spawn,mission_propose", "org_spawn,mission_propose,mission_plan,mission_status"
	if got := turn("t1", "--mission-token=forged"); got != proposeOnly {
		t.Errorf("a token with no record: %s", got)
	}
	c, _ := cs.Confirm("default", conv.ID, "t2", 0)
	if got := turn("t2", "--mission-token="+c.Nonce); got != planning {
		t.Errorf("the operator's confirmation: %s", got)
	}
	if got := turn("t3", "--mission-token="+c.Nonce); got != proposeOnly {
		t.Errorf("the consumed token on a later turn: %s", got)
	}
	if _, ok := cs.PeekConfirm("default", conv.ID, "t2"); ok {
		t.Error("the confirmation survived its turn")
	}
}
