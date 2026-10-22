package chat

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/ai"
	"github.com/monoes/mono-agent/internal/ai/chatevents"
	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/storage"
)

// accessTranscript copies the turn's system prompt next to the argv log, so a
// test can read what the lead was told.
const accessTranscript = `  prev=""; for a in "$@"; do if [ "$prev" = "--system-file" ]; then cp "$a" "$(dirname "$0")/system.txt"; fi; prev="$a"; done
` + successTranscript

type accessRig struct {
	dbPath, bin, argsLog string
	store                *ai.AIStore
}

func newAccessRig(t *testing.T, enabled bool) *accessRig {
	t.Helper()
	asOperator(t)
	r := &accessRig{dbPath: newChatCLITestDB(t)}
	r.bin, r.argsLog = writeCoderMonomind(t, accessTranscript)
	withCoderCaps(t, monomind.CoderCapabilities...)
	if enabled {
		setCoderSettings(t, r.dbPath, coderSettings{Enabled: true})
	}
	r.store = openTestChatStore(t, r.dbPath)
	return r
}

func (r *accessRig) conv(t *testing.T, access string) ai.Conversation {
	t.Helper()
	cwd := ""
	if access != ai.AccessScoped {
		cwd = t.TempDir()
	}
	c, err := r.store.CreateConversationAccess("default", "agent", "general", "claude", "", "", access, cwd, "")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// exec is the last `agent exec` argv line the fake monomind logged, "" if none.
func (r *accessRig) exec() string {
	logged, _ := os.ReadFile(r.argsLog)
	line := ""
	for _, l := range strings.Split(string(logged), "\n") {
		if strings.HasPrefix(l, "agent exec") {
			line = l
		}
	}
	return line
}

func (r *accessRig) system() string {
	b, _ := os.ReadFile(filepath.Join(filepath.Dir(r.argsLog), "system.txt"))
	return string(b)
}

// ran starts over the argv log and the captured prompt.
func (r *accessRig) reset() {
	_ = os.Remove(r.argsLog)
	_ = os.Remove(filepath.Join(filepath.Dir(r.argsLog), "system.txt"))
}

// The level chooses the turn: scoped is the assistant's (no full access, no
// coder folder), every other level is a coder turn in the conversation's
// folder with full access; coding is told nothing about the profile while
// workspace and full are.
func TestTheLevelChoosesTheTurn(t *testing.T) {
	r := newAccessRig(t, true)
	for _, access := range ai.AccessLevels() {
		r.reset()
		c := r.conv(t, access)
		out, err := runChatCmd(t, r.dbPath, r.bin, "--conversation", c.ID, "--turn", "turn-"+access, "--", "hi")
		if err != nil {
			t.Fatalf("%s turn: %v\n%s", access, err, out)
		}
		line := r.exec()
		coder := access != ai.AccessScoped
		if coder != strings.Contains(line, "--access full") || coder != strings.Contains(line, "--cwd "+c.Cwd) {
			t.Errorf("%s: coder turn = %v, argv %s", access, coder, line)
		}
		if coder {
			if got := strings.Contains(r.system(), profileSummaryText); got != (access != ai.AccessCoding) {
				t.Errorf("%s: profile summary in the prompt = %v", access, got)
			}
		}
		if got := browserKindFor(access); (got == browserctl.KindCoder) != coder || (got == browserctl.KindChat) == coder {
			t.Errorf("%s runs as browser kind %q", access, got)
		}
		_, err = runChatCmd(t, r.dbPath, r.bin, "--conversation", c.ID, "--turn", "tools-"+access, "--tools", "monoagent:read", "--", "hi")
		if coder && exitCodeFor(err) != 3 {
			t.Errorf("%s took --tools: %v", access, err)
		}
		if !coder && err != nil {
			t.Errorf("a scoped conversation refused its page-read tools: %v", err)
		}
	}
}

// With coder mode off only scoped can be chosen or created, and a turn above
// scoped is refused; the scoped one still runs.
func TestCoderGateAppliesAboveScopedOnly(t *testing.T) {
	r := newAccessRig(t, false)
	scoped := r.conv(t, ai.AccessScoped)
	if out, err := runChatCmd(t, r.dbPath, r.bin, "--conversation", scoped.ID, "--turn", "s1", "--", "hi"); err != nil {
		t.Fatalf("scoped turn with coder off: %v\n%s", err, out)
	}
	for _, access := range []string{ai.AccessCoding, ai.AccessWorkspace, ai.AccessFull} {
		c := r.conv(t, access)
		var ce *coderError
		out, err := runChatCmd(t, r.dbPath, r.bin, "--conversation", c.ID, "--turn", "c-"+access, "--", "hi")
		if !asCoderError(err, &ce) || ce.code != "coder_disabled" {
			t.Errorf("%s turn with coder off: %v\n%s", access, err, out)
		}
		out, code := runChatHistory(t, r.dbPath, "default", "create", "--runtime", "claude", "--access", access, "--new-workspace")
		if code != 3 || !strings.Contains(out, `"code":"coder_disabled"`) {
			t.Errorf("create --access %s with coder off: exit %d %s", access, code, out)
		}
		if out, code := runChatHistory(t, r.dbPath, "default", "set-access", scoped.ID, access, "--new-workspace"); code != 3 || !strings.Contains(out, `"code":"coder_disabled"`) {
			t.Errorf("set-access %s with coder off: exit %d %s", access, code, out)
		}
	}
	if out, code := runChatHistory(t, r.dbPath, "default", "create", "--runtime", "claude", "--access", "scoped"); code != 0 {
		t.Errorf("create scoped with coder off: exit %d %s", code, out)
	}
	if got, _ := r.store.GetConversation(scoped.ID, "default"); got.Access != ai.AccessScoped {
		t.Errorf("a refused set-access moved the conversation to %q", got.Access)
	}
}

// --mode stays: assistant is scoped, coder is full unless --access says more,
// naming both must agree, and the JSON has both keys.
func TestCreateKeepsModeAsAnAlias(t *testing.T) {
	r := newAccessRig(t, true)
	cases := []struct {
		args         []string
		access, mode string
		code         int
	}{
		{[]string{}, "scoped", "assistant", 0},
		{[]string{"--mode", "assistant"}, "scoped", "assistant", 0},
		{[]string{"--mode", "coder", "--new-workspace"}, "full", "coder", 0},
		{[]string{"--access", "coding", "--new-workspace"}, "coding", "coder", 0},
		{[]string{"--access", "workspace", "--mode", "coder", "--new-workspace"}, "workspace", "coder", 0},
		{[]string{"--access", "full", "--mode", "assistant", "--new-workspace"}, "", "", 3},
		{[]string{"--access", "root"}, "", "", 3},
		{[]string{"--access", "scoped", "--cwd", t.TempDir()}, "", "", 3},
	}
	for _, c := range cases {
		var got ai.ConversationRecord
		out, code := runChatHistory(t, r.dbPath, "default", append([]string{"create", "--runtime", "claude"}, c.args...)...)
		if code != c.code {
			t.Errorf("create %v: exit %d %s", c.args, code, out)
			continue
		}
		if c.code != 0 {
			continue
		}
		decodeChatJSON(t, out, &got)
		var keys map[string]any
		decodeChatJSON(t, out, &keys)
		if got.Access != c.access || got.Mode != c.mode || keys["access"] != c.access || keys["mode"] != c.mode {
			t.Errorf("create %v = %s", c.args, out)
		}
	}
}

// background starts a process the way a turn leaves one: carrying the turn's
// exec-tree token, which is what makes it the one `stop-background` may stop.
func background(t *testing.T) int {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("a process's identity comes from /proc")
	}
	c := exec.Command("sleep", "60")
	c.Env = append(os.Environ(), execTreeEnv+"=tok-"+t.Name())
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := make(chan struct{})
	go func() { c.Wait(); close(reaped) }()
	t.Cleanup(func() { c.Process.Kill(); <-reaped })
	return c.Process.Pid
}

// leaveBehind records a turn that ended with pid still running.
func (r *accessRig) leaveBehind(t *testing.T, convID string, pid int) {
	t.Helper()
	if _, _, err := r.store.CreateTurn(convID, "default", "left-1", "i", "go"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.AppendEvent("default", convID, "left-1", chatevents.EventNotice, backgroundNotice([]int{pid})); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.store.FinalizeTurn("default", convID, "left-1", chatevents.StatusCompleted, "", nil, false); err != nil {
		t.Fatal(err)
	}
}

func gone(pid int) bool {
	for i := 0; i < 50; i++ {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// Lowering is anyone's and takes the privileges before the next turn: the
// process the conversation's turns left running is stopped, its kept org
// workers are forgotten and its open task's token stops working; the next
// turn is the assistant's.
func TestLoweringTakesThePrivilegesBeforeTheNextTurn(t *testing.T) {
	r := newAccessRig(t, true)
	c := r.conv(t, ai.AccessFull)
	pid := background(t)
	r.leaveBehind(t, c.ID, pid)
	if err := r.store.SaveOrgWorker("default", c.ID, ai.OrgWorker{AgentID: "w1", TurnID: "left-1", Runtime: "claude", SessionID: "s"}); err != nil {
		t.Fatal(err)
	}
	db, err := storage.NewDatabase(r.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ts := &aitask.Store{DB: db.DB, Profile: "default"}
	task, err := ts.Insert(aitask.Task{ID: aitask.NewID(), Kind: "generic", ConversationID: c.ID, Status: aitask.StatusRunning, Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	token := aitask.NewToken()
	if err := ts.SetToken(task.ID, token); err != nil {
		t.Fatal(err)
	}

	// An agent's call (the operator check refuses it) may lower.
	out, err := r.setAccess(t, errNotOperator, c.ID, "scoped")
	if err != nil {
		t.Fatalf("an agent lowering: %v\n%s", err, out)
	}
	var change accessChange
	decodeChatJSON(t, out, &change)
	if change.Access != "scoped" || change.Previous != "full" || change.Mode != "assistant" || change.Lowered == nil {
		t.Errorf("set-access output: %s", out)
	}
	if !gone(pid) {
		t.Error("the process the conversation left running survived the lowering")
	}
	if ws, _ := r.store.ListOrgWorkers("default", c.ID, 0); len(ws) != 0 {
		t.Errorf("kept org workers survived the lowering: %+v", ws)
	}
	if ts.CheckToken(task.ID, token) {
		t.Error("the task's token still works after the lowering")
	}
	r.reset()
	if out, err := runChatCmd(t, r.dbPath, r.bin, "--conversation", c.ID, "--turn", "after", "--", "hi"); err != nil || strings.Contains(r.exec(), "--access full") {
		t.Fatalf("the next turn after a lowering is not the assistant's: %v\n%s\n%s", err, out, r.exec())
	}
}

// A level that went down while a turn ran loses what the turn left behind
// when it ends; one that did not keeps it.
func TestEnforceLoweredAccessAtTheEndOfATurn(t *testing.T) {
	r := newAccessRig(t, true)
	c := r.conv(t, ai.AccessFull)
	pid := background(t)
	r.leaveBehind(t, c.ID, pid)
	deps := testDeps(&testConfig{DBPath: r.dbPath, ProfileID: "default"})

	enforceLoweredAccess(deps, r.store, "default", c)
	if gone(pid) {
		t.Fatal("a conversation whose level did not change lost its process")
	}
	if _, err := r.store.SetConversationAccess(c.ID, "default", ai.AccessCoding, ""); err != nil {
		t.Fatal(err)
	}
	enforceLoweredAccess(deps, r.store, "default", c)
	if !gone(pid) {
		t.Error("a conversation lowered during its turn kept the process the turn left")
	}
}

// Raising is the operator's, needs a folder for a scoped conversation, keeps a
// folder it has, and starts a fresh session.
func TestRaisingNeedsTheOperatorAndAFolder(t *testing.T) {
	r := newAccessRig(t, true)
	c := r.conv(t, ai.AccessScoped)
	if out, err := r.setAccess(t, errNotOperator, c.ID, "coding", "--new-workspace"); exitCodeFor(err) != 3 || !strings.Contains(out+err.Error(), "operator") {
		t.Fatalf("an agent raised the level: %v\n%s", err, out)
	}
	if got, _ := r.store.GetConversation(c.ID, "default"); got.Access != ai.AccessScoped {
		t.Fatalf("a refused raise moved the conversation to %q", got.Access)
	}
	if _, err := r.setAccess(t, nil, c.ID, "coding"); exitCodeFor(err) != 3 {
		t.Errorf("raising a scoped conversation without a folder: %v", err)
	}
	if err := r.store.BindConversationSession(c.ID, "default", "claude", "sess-1"); err != nil {
		t.Fatal(err)
	}
	out, err := r.setAccess(t, nil, c.ID, "workspace", "--new-workspace")
	if err != nil {
		t.Fatalf("operator raise: %v\n%s", err, out)
	}
	got, _ := r.store.GetConversation(c.ID, "default")
	if got.Access != ai.AccessWorkspace || got.Mode != ai.ModeCoder || got.Cwd == "" || got.SessionID != "" {
		t.Errorf("after the raise: %+v", got)
	}
	if _, err := r.setAccess(t, nil, c.ID, "full", "--new-workspace"); exitCodeFor(err) != 3 {
		t.Errorf("a conversation with a folder was given another: %v", err)
	}
	if _, err := r.setAccess(t, nil, c.ID, "full"); err != nil {
		t.Errorf("raising within the coder levels: %v", err)
	}
	if got, _ := r.store.GetConversation(c.ID, "default"); got.Access != ai.AccessFull || got.Cwd == "" {
		t.Errorf("after full: %+v", got)
	}
}

var errNotOperator = clicore.OperatorOnly("only the user can do that: an agent is running this")

func asCoderError(err error, target **coderError) bool { return errors.As(err, target) }

// setAccess runs `chat history set-access` with operator as the answer of the
// operator check (nil is the user).
func (r *accessRig) setAccess(t *testing.T, operator error, args ...string) (string, error) {
	t.Helper()
	cfg := testDeps(&testConfig{DBPath: r.dbPath, ProfileID: "default", JSONOutput: true})
	cfg.RequireOperator = func(string) error { return operator }
	cmd := newChatCmd(cfg)
	cmd.SetArgs(append([]string{"history", "set-access"}, args...))
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	var err error
	out := captureStdout(t, func() { err = cmd.Execute() })
	return out, err
}
