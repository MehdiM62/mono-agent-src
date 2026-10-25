package chat

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/ai"
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
	cmds := pendingCommands(t, dbPath) // control commands come first
	if len(cmds) != 2 || cmds[0].Kind != coderhost.KindPause || cmds[1].Kind != coderhost.KindSend || cmds[1].Message != "do the thing" {
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

// A host-started turn is detached, takes its prompt on stdin, is found again by
// pid and stamp after a host restart, and is stopped through its process group.
func TestExecLauncherStartsADetachedTurnAndStopsItsGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups")
	}
	dir := t.TempDir()
	argsLog := filepath.Join(dir, "args.log")
	promptLog := filepath.Join(dir, "prompt.txt")
	bin := filepath.Join(dir, "fake-monoagentcli")
	script := "#!/bin/sh\necho \"$*\" >> '" + argsLog + "'\nif [ \"$4\" = \"chat\" ] && [ \"$5\" = \"history\" ]; then exit 0; fi\n" +
		"cat > '" + promptLog + "'\necho '{\"admitted\":true,\"existed\":false,\"turn\":{\"id\":\"t1\"}}'\nsleep 60 &\nwait\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	l := &execLauncher{bin: bin, dbPath: "/db/x.db", instance: "host-test", logDir: filepath.Join(dir, "turns")}

	if _, err := l.Start("p", "-rf", "t1", "hi", nil); err == nil {
		t.Fatal("a conversation id that reads as a flag was accepted")
	}
	secret := strings.Repeat("a long prompt ", 20000) // 280 KB: over the 128 KiB argument limit
	turn, err := l.Start("p", "c1", "t1", secret, []string{"/tmp/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if turn.PID() <= 0 || turn.Stamp() == "" {
		t.Fatalf("pid %d stamp %q", turn.PID(), turn.Stamp())
	}
	if l.Adopt(turn.PID(), turn.Stamp()) == nil {
		t.Fatal("a live turn was not found again")
	}
	if l.Adopt(turn.PID(), "start:0") != nil || l.Adopt(turn.PID(), "") != nil || l.Adopt(1, "start:1") != nil {
		t.Fatal("a pid with a different or no start time, or pid 1, was adopted")
	}
	b, _ := os.ReadFile(argsLog)
	if !strings.Contains(string(b), "--db-path /db/x.db --profile p --json chat --conversation c1 --turn t1 --instance host-test --prompt-stdin --attach=/tmp/a.txt") || strings.Contains(string(b), "a long prompt") {
		t.Fatalf("arguments = %s (the prompt must not be one)", b)
	}
	waitUntil(t, "the prompt to arrive on stdin", func() bool {
		got, _ := os.ReadFile(promptLog)
		return string(got) == secret
	})
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
	if _, err := os.Stat(filepath.Join(dir, "turns", "t1.prompt")); !os.IsNotExist(err) {
		t.Fatalf("the prompt file was left behind: %v", err)
	}
	l.Finish("p", "c1", "t1", "gone")
	if b, _ := os.ReadFile(argsLog); !strings.Contains(string(b), "chat history finish c1 t1 --status failed --reason gone") {
		t.Fatalf("finish not run: %s", b)
	}
}

// A turn the process refuses before admitting it (the app owns the chat, no such
// conversation, a turn id used before) fails the command with the reason.
func TestExecLauncherReportsARefusedTurn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-monoagentcli")
	script := "#!/bin/sh\ncat > /dev/null\necho '{\"error\":\"chat: conversation has an active turn owned by another instance\",\"code\":\"invalid_input\"}'\nexit 3\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	l := &execLauncher{bin: bin, dbPath: "/db/x.db", instance: "host-test", logDir: filepath.Join(dir, "turns")}
	_, err := l.Start("p", "c1", "t1", "hello", nil)
	if err == nil || !strings.Contains(err.Error(), "owned by another instance") {
		t.Fatalf("Start = %v, want the refusal", err)
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

// The app's startup sweep must not interrupt a turn whose process the host still
// has, but must interrupt one whose process is gone (a host that crashed).
func TestReconcileSparesOnlyTurnsWhoseProcessIsAlive(t *testing.T) {
	if runtime.GOOS == "windows" || pidStamp(os.Getpid()) == "" {
		t.Skip("no process start times here")
	}
	dbPath := newChatCLITestDB(t)
	store := openTestChatStore(t, dbPath)
	conv := map[string]string{}
	for _, turn := range []string{"alive", "dead", "nostamp", "orphan"} {
		c, _ := store.CreateConversation("default", "agent", "general", "claude", "", "")
		store.CreateTurn(c.ID, "default", turn, "host-abc", "hi")
		conv[turn] = c.ID
	}
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Skip("no `true` to start a short-lived process")
	}
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	cs := coderhost.NewStore(db.DB)
	set := func(turn string, pid int, stamp string) {
		x, _ := cs.EnsureSession("default", conv[turn])
		x.State, x.TurnID, x.TurnPID, x.TurnStamp = coderhost.StateRunning, turn, pid, stamp
		if err := cs.SaveSession(x); err != nil {
			t.Fatal(err)
		}
	}
	set("alive", os.Getpid(), pidStamp(os.Getpid()))
	set("dead", gone.Process.Pid, "start:1") // a host that crashed: its turn's process is gone
	set("nostamp", os.Getpid(), "")          // cannot tell this process from a reused pid
	db.Close()

	if out, code := runChatHistory(t, dbPath, "default", "reconcile", "--except-owner", "me"); code != 0 {
		t.Fatalf("reconcile exit %d: %s", code, out)
	}
	for turn, want := range map[string]string{"alive": "active", "dead": "interrupted", "nostamp": "interrupted", "orphan": "interrupted"} {
		if got, _ := store.GetTurn(turn, "default"); got.Status != want {
			t.Errorf("turn %s = %q, want %q", turn, got.Status, want)
		}
	}
}
