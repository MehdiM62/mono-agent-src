package chat

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/ai"
	"github.com/monoes/mono-agent/internal/ai/chatevents"
	"github.com/monoes/mono-agent/internal/coderhost"
	"github.com/monoes/mono-agent/internal/daemonhb"
	"github.com/monoes/mono-agent/internal/storage"
)

func runSessionsCLI(t *testing.T, cfg *Deps, args ...string) (string, int) {
	t.Helper()
	cmd := newCoderSessionsCmd(cfg)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(args)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	err := cmd.Execute()
	return out.String(), exitCodeFor(err)
}

func pendingCommands(t *testing.T, dbPath string) []coderhost.Command {
	t.Helper()
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cmds, err := coderhost.NewStore(db.DB).Pending()
	if err != nil {
		t.Fatal(err)
	}
	return cmds
}

func TestSessionsSendQueuesForTheHostAndStartsOne(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	store := openTestChatStore(t, dbPath)
	conv, _ := store.CreateConversationMode("default", "agent", "general", "claude", "", "", ai.ModeCoder, t.TempDir())
	scoped, _ := store.CreateConversation("default", "agent", "general", "claude", "", "")
	cfg := testDeps(&testConfig{DBPath: dbPath, ProfileID: "default", JSONOutput: true})

	started := 0
	orig := startHostProcess
	t.Cleanup(func() { startHostProcess = orig })
	startHostProcess = func(c *Deps) error { // stands in for the detached host: takes the lock
		started++
		release, err := daemonhb.LockFile(hostLockPath(c))
		t.Cleanup(func() { release() })
		return err
	}

	if out, code := runSessionsCLI(t, cfg, "send", conv.ID, "do the thing"); code != 0 || !strings.Contains(out, `"turnId"`) {
		t.Fatalf("send: exit %d %s", code, out)
	}
	if started != 1 || !hostRunning(cfg) {
		t.Fatalf("host started %d times, running=%v", started, hostRunning(cfg))
	}
	if _, code := runSessionsCLI(t, cfg, "pause", conv.ID); code != 0 || started != 1 {
		t.Fatalf("a running host is not started again: exit %d, starts %d", code, started)
	}
	cmds := pendingCommands(t, dbPath)
	if len(cmds) != 2 || cmds[0].Kind != coderhost.KindSend || cmds[0].Message != "do the thing" || cmds[1].Kind != coderhost.KindPause {
		t.Fatalf("queued = %+v", cmds)
	}
	if _, code := runSessionsCLI(t, cfg, "send", scoped.ID, "x"); code == 0 {
		t.Fatal("a scoped chat is not a coder session")
	}
	if _, code := runSessionsCLI(t, cfg, "send", "no-such-chat", "x"); code != 4 && code == 0 {
		t.Fatalf("unknown conversation: exit %d", code)
	}
	if len(pendingCommands(t, dbPath)) != 2 {
		t.Fatal("a refused send was queued")
	}
}

func TestSessionsCommandsAreTheOperatorsOnly(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	conv, _ := openTestChatStore(t, dbPath).CreateConversationMode("default", "agent", "general", "claude", "", "", ai.ModeCoder, t.TempDir())
	cfg := testDeps(&testConfig{DBPath: dbPath, ProfileID: "default", JSONOutput: true})
	cfg.RequireOperator = func(what string) error { return errors.New("operator only: " + what) }
	orig := startHostProcess
	t.Cleanup(func() { startHostProcess = orig })
	startHostProcess = func(*Deps) error { t.Error("an agent started the host"); return nil }
	for _, args := range [][]string{{"send", conv.ID, "x"}, {"stop", conv.ID}, {"pause", conv.ID}, {"resume", conv.ID}} {
		if _, code := runSessionsCLI(t, cfg, args...); code == 0 {
			t.Errorf("%v was allowed for a non-operator", args)
		}
	}
	if len(pendingCommands(t, dbPath)) != 0 {
		t.Fatal("a refused command was queued")
	}
	cmd := newCoderHostCmd(cfg)
	cmd.SetArgs([]string{"run"})
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	if err := cmd.Execute(); err == nil || hostRunning(cfg) {
		t.Fatalf("host run for a non-operator: err=%v running=%v", err, hostRunning(cfg))
	}
}

// A host-started turn is detached, found again by pid and stamp after a host
// restart, and stopped through its process group.
func TestExecLauncherStartsADetachedTurnAndStopsItsGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups")
	}
	dir := t.TempDir()
	argsLog := filepath.Join(dir, "args.log")
	bin := filepath.Join(dir, "fake-monoagentcli")
	script := "#!/bin/sh\necho \"$*\" >> '" + argsLog + "'\nif [ \"$4\" = \"chat\" ] && [ \"$5\" = \"history\" ]; then exit 0; fi\nsleep 60 &\nwait\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	l := &execLauncher{bin: bin, dbPath: "/db/x.db", instance: "host-test", logDir: filepath.Join(dir, "turns")}

	if _, err := l.Start("p", "-rf", "t1", "hi", nil); err == nil {
		t.Fatal("a conversation id that reads as a flag was accepted")
	}
	turn, err := l.Start("p", "c1", "t1", "hello world", []string{"/tmp/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if turn.PID() <= 0 || turn.Stamp() == "" {
		t.Fatalf("pid %d stamp %q", turn.PID(), turn.Stamp())
	}
	if l.Adopt(turn.PID(), turn.Stamp()) == nil {
		t.Fatal("a live turn was not found again")
	}
	if l.Adopt(turn.PID(), "start:0") != nil {
		t.Fatal("a pid with a different start time was adopted")
	}
	waitUntil(t, "the fake CLI to log its arguments", func() bool {
		b, _ := os.ReadFile(argsLog)
		return strings.Contains(string(b), "--conversation c1 --turn t1 --instance host-test --attach=/tmp/a.txt -- hello world")
	})
	if b, _ := os.ReadFile(argsLog); !strings.Contains(string(b), "--db-path /db/x.db --profile p --json chat") {
		t.Fatalf("global flags missing: %s", b)
	}
	done := make(chan struct{})
	go func() { _ = turn.Wait(); close(done) }()
	turn.Stop()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a stopped turn did not end")
	}
	if l.Adopt(turn.PID(), turn.Stamp()) != nil {
		t.Fatal("an ended turn was adopted")
	}
	l.Finish("p", "c1", "t1", "gone")
	if b, _ := os.ReadFile(argsLog); !strings.Contains(string(b), "chat history finish c1 t1 --status failed --reason gone") {
		t.Fatalf("finish not run: %s", b)
	}
}

func waitUntil(t *testing.T, what string, f func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if f() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// The app's startup sweep must not interrupt a turn the coder host is running.
func TestReconcileSparesTurnsTheHostHolds(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	store := openTestChatStore(t, dbPath)
	held, _ := store.CreateConversation("default", "agent", "general", "claude", "", "")
	orphan, _ := store.CreateConversation("default", "agent", "general", "claude", "", "")
	store.CreateTurn(held.ID, "default", "held", "host-abc", "hi")
	store.CreateTurn(orphan.ID, "default", "orphan", "dead-instance", "hi")

	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	cs := coderhost.NewStore(db.DB)
	x, _ := cs.EnsureSession("default", held.ID)
	x.State, x.TurnID, x.TurnPID = coderhost.StateRunning, "held", 4242
	if err := cs.SaveSession(x); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if out, code := runChatHistory(t, dbPath, "default", "reconcile", "--except-owner", "me"); code != 0 {
		t.Fatalf("reconcile exit %d: %s", code, out)
	}
	for id, want := range map[string]string{"held": "active", "orphan": string(chatevents.StatusInterrupted)} {
		if got, _ := store.GetTurn(id, "default"); got.Status != want {
			t.Errorf("turn %s = %q, want %q", id, got.Status, want)
		}
	}
}
