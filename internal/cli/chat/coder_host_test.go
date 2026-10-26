package chat

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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
	cmds := pendingCommands(t, dbPath) // in the order queued
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
		"cat > '" + promptLog + "'\necho '{\"level\":\"warn\",\"error\":\"a library log line, not a refusal\"}'\necho '{\"admitted\":true,\"existed\":false,\"turn\":{\"id\":\"t1\"}}'\n" +
		"(trap '' TERM; exec sleep 60) &\necho $! > '" + filepath.Join(dir, "child.pid") + "'\nwait\n"
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
	childPID := 0
	if b, err := os.ReadFile(filepath.Join(dir, "child.pid")); err == nil {
		childPID, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	if childPID <= 1 {
		t.Fatalf("no child pid recorded")
	}
	waitUntil(t, "the child that ignores SIGTERM to be killed with its leader", func() bool { return !daemonhb.ProcessAlive(childPID) })
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

// A turn another host started has no waiter to remove its prompt: Finish does.
func TestFinishRemovesTheTurnsPromptFile(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	l := &execLauncher{bin: bin, dbPath: "/db/x.db", instance: "host-test", logDir: filepath.Join(dir, "turns")}
	os.MkdirAll(l.logDir, 0o700)
	prompt := filepath.Join(l.logDir, "t1.prompt")
	os.WriteFile(prompt, []byte("secret"), 0o600)
	l.Finish("p", "c1", "t1", "gone")
	if _, err := os.Stat(prompt); !os.IsNotExist(err) {
		t.Fatalf("prompt file left behind: %v", err)
	}
}

// While the host is still admitting a turn (recorded, no pid yet) the sweep
// spares it, for a while; a record that stays that way is an orphan. When the
// host's records cannot be read, no host turn is interrupted.
func TestHostHeldTurnsCoversTheAdmissionWindowAndFailsClosed(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	store := openTestChatStore(t, dbPath)
	fresh, _ := store.CreateConversation("default", "agent", "general", "claude", "", "")
	stale, _ := store.CreateConversation("default", "agent", "general", "claude", "", "")
	store.CreateTurn(fresh.ID, "default", "fresh", "host-abc", "hi")
	store.CreateTurn(stale.ID, "default", "stale", "host-abc", "hi")
	db, _ := storage.NewDatabase(dbPath)
	cs := coderhost.NewStore(db.DB)
	for _, c := range []struct{ conv, turn string }{{fresh.ID, "fresh"}, {stale.ID, "stale"}} {
		x, _ := cs.EnsureSession("default", c.conv)
		x.State, x.TurnID = coderhost.StateRunning, c.turn
		cs.SaveSession(x)
	}
	db.DB.Exec(`UPDATE coder_sessions SET updated_at = ? WHERE conversation_id = ?`, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano), stale.ID)
	db.Close()

	cfg := testDeps(&testConfig{DBPath: dbPath, ProfileID: "default"})
	skip, closeSkip := hostHeldTurns(cfg)
	defer closeSkip()
	if !skip(ai.Turn{ID: "fresh", ConversationID: fresh.ID, ProfileID: "default"}) {
		t.Error("a turn the host is admitting right now was not spared")
	}
	if skip(ai.Turn{ID: "stale", ConversationID: stale.ID, ProfileID: "default"}) {
		t.Error("a turn stuck in admission for an hour was spared")
	}

	broken := testDeps(&testConfig{DBPath: dbPath, ProfileID: "default"})
	broken.OpenDB = func() (*storage.Database, error) { return nil, errors.New("disk on fire") }
	skip, closeSkip2 := hostHeldTurns(broken)
	defer closeSkip2()
	if !skip(ai.Turn{ID: "x", OwnerInstanceID: "host-abc"}) || skip(ai.Turn{ID: "y", OwnerInstanceID: "11111111-2222-3333-4444-555555555555"}) {
		t.Error("with unreadable records the sweep must spare host-owned turns and only those")
	}
}

// reapedChild starts a long-lived child and reaps it when it ends, so only a
// zombie the test makes on purpose is one.
func reapedChild(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skip("no sleep")
	}
	go cmd.Wait()
	t.Cleanup(func() { cmd.Process.Kill() })
	return cmd
}

// A start time that cannot be read for a moment (ps failed, /proc denied) is not
// an exit: reading it as one would start the next turn beside a running agent.
func TestAnAdoptedTurnWhoseStartTimeCannotBeReadIsStillRunning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process start times differ")
	}
	child := reapedChild(t)
	l := &execLauncher{}
	stamp := pidStamp(child.Process.Pid)
	if stamp == "" {
		t.Skip("no process start times here")
	}
	turn := l.Adopt(child.Process.Pid, stamp)
	if turn == nil {
		t.Fatal("a live turn was not adopted")
	}
	oldStamp, oldPoll := stampOf, adoptedPoll
	stampOf, adoptedPoll = func(int) string { return "" }, 20*time.Millisecond
	t.Cleanup(func() { stampOf, adoptedPoll = oldStamp, oldPoll })
	if l.Adopt(child.Process.Pid, stamp) == nil {
		t.Fatal("a live turn was dropped because its start time could not be read")
	}
	ended := make(chan struct{})
	go func() { _ = turn.Wait(); close(ended) }()
	select {
	case <-ended:
		t.Fatal("Wait returned for a live process whose start time could not be read")
	case <-time.After(300 * time.Millisecond):
	}
	child.Process.Kill()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return after the process died")
	}
}

// A turn the host cannot identify could never be found again, spared by the
// startup sweep or stopped: it is not left running.
func TestAStartedTurnWithoutAReadableStartTimeIsStoppedAndFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	bin := filepath.Join(dir, "fake")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho $$ > '"+pidFile+"'\ncat > /dev/null\necho '{\"admitted\":true}'\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldStamp := stampOf
	stampOf = func(int) string { return "" }
	t.Cleanup(func() { stampOf = oldStamp })
	l := &execLauncher{bin: bin, dbPath: "/db/x.db", instance: "host-test", logDir: filepath.Join(dir, "turns")}
	_, err := l.Start("p", "c1", "t1", "hello", nil)
	if err == nil || !strings.Contains(err.Error(), "identify") {
		t.Fatalf("Start = %v, want a failure that says the process could not be identified", err)
	}
	waitUntil(t, "the unidentifiable turn's process to be gone", func() bool {
		b, _ := os.ReadFile(pidFile)
		pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		return pid > 1 && !turnAlive(pid)
	})
}

// An exited leader nobody reaped is a zombie: kill(pid, 0) still accepts it, but
// it can never run again, so the turn is over.
func TestAZombieIsNotAliveForTheHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no zombies")
	}
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Skip("no true")
	}
	pid := cmd.Process.Pid
	waitUntil(t, "the child to become a zombie", func() bool { return processState(pid) == "Z" })
	defer cmd.Wait()
	if !daemonhb.ProcessAlive(pid) {
		t.Skip("this platform's ProcessAlive already says a zombie is dead")
	}
	if turnAlive(pid) {
		t.Fatal("a zombie counts as a running turn")
	}
	if (&execLauncher{}).Adopt(pid, "start:1@x") != nil {
		t.Fatal("a zombie was adopted")
	}
	zombie := &adoptedTurn{pid: pid, stamp: "start:1@x"}
	done := make(chan struct{})
	go func() { _ = zombie.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait never returned for a zombie leader")
	}
}

// The prompt file is gone as soon as the turn runs (it holds the open file), and
// the turn still reads the whole prompt.
func TestThePromptFileIsRemovedOnceTheTurnHoldsIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows removes it after the turn")
	}
	dir := t.TempDir()
	promptLog := filepath.Join(dir, "prompt.txt")
	bin := filepath.Join(dir, "fake")
	script := "#!/bin/sh\necho '{\"admitted\":true}'\nsleep 1\ncat > '" + promptLog + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	l := &execLauncher{bin: bin, dbPath: "/db/x.db", instance: "host-test", logDir: filepath.Join(dir, "turns")}
	turn, err := l.Start("p", "c1", "t1", "the whole prompt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "turns", "t1.prompt")); !os.IsNotExist(err) {
		t.Fatalf("the prompt file is still on disk while the turn runs: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "turns")); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("log dir = %v, %v", fi, err)
	}
	_ = turn.Wait()
	if b, _ := os.ReadFile(promptLog); string(b) != "the whole prompt" {
		t.Fatalf("the turn read %q", b)
	}
}

func TestSweepPromptsRemovesOnlyOldFiles(t *testing.T) {
	dir := t.TempDir()
	oldF, newF, other := filepath.Join(dir, "old.prompt"), filepath.Join(dir, "new.prompt"), filepath.Join(dir, "keep.log")
	for _, f := range []string{oldF, newF, other} {
		os.WriteFile(f, []byte("x"), 0o600)
	}
	past := time.Now().Add(-3 * time.Hour)
	os.Chtimes(oldF, past, past)
	os.Chtimes(other, past, past)
	sweepPrompts(dir, time.Hour)
	for f, want := range map[string]bool{oldF: false, newF: true, other: true} {
		if _, err := os.Stat(f); (err == nil) != want {
			t.Errorf("%s exists=%v, want %v", filepath.Base(f), err == nil, want)
		}
	}
}

// host stop signals only a process it can tell is the host: a host.json with no
// start time (an older host, or one that could not read it) is refused.
func TestHostStopRefusesWhatItCannotIdentify(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	cfg := testDeps(&testConfig{DBPath: dbPath, ProfileID: "default", JSONOutput: true})
	child := reapedChild(t)
	if err := os.MkdirAll(hostDir(cfg), 0o700); err != nil {
		t.Fatal(err)
	}
	release, err := daemonhb.LockFile(hostLockPath(cfg)) // a "host" is running
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	raw, _ := json.Marshal(hostInfo{PID: child.Process.Pid, Stamp: "", Instance: "host-x"})
	os.WriteFile(filepath.Join(hostDir(cfg), hostInfoFile), raw, 0o600)
	cmd := newCoderHostCmd(cfg)
	cmd.SetArgs([]string{"stop"})
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err == nil {
		t.Fatal("host stop signalled a process it could not identify")
	}
	time.Sleep(100 * time.Millisecond)
	if !turnAlive(child.Process.Pid) {
		t.Fatal("the unidentified process was killed")
	}
}
