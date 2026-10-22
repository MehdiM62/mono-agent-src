package chat

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/monoes/mono-agent/internal/ai"
	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/storage"
)

type taskRig struct {
	dbPath, convID, envLog string
	bin                    string
}

// newTaskRig has a coder conversation and a fake monomind that records the
// AI-task environment its turn ran with.
func newTaskRig(t *testing.T) *taskRig {
	t.Helper()
	r := &taskRig{dbPath: newChatCLITestDB(t)}
	r.envLog = filepath.Join(t.TempDir(), "env.log")
	r.bin, _ = writeCoderMonomind(t, `  echo "ENV [$MONOAGENT_AITASK_ID] [$MONOAGENT_AITASK_TOKEN]" >> '`+r.envLog+`'
`+coderTranscript)
	withCoderCaps(t, monomind.CoderCapabilities...)
	setCoderSettings(t, r.dbPath, coderSettings{Enabled: true})
	conv, err := openTestChatStore(t, r.dbPath).CreateConversationMode("default", "agent", "general", "claude", "", "", ai.ModeCoder, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.convID = conv.ID
	return r
}

func (r *taskRig) store(t *testing.T, profile string) (*aitask.Store, func()) {
	t.Helper()
	db, err := storage.NewDatabase(r.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	return &aitask.Store{DB: db.DB, Profile: profile}, func() { db.Close() }
}

// task inserts a task on the rig's conversation with a token of its own.
func (r *taskRig) task(t *testing.T, profile, status string) (aitask.Task, string) {
	t.Helper()
	st, done := r.store(t, profile)
	defer done()
	task, err := st.Insert(aitask.Task{ID: aitask.NewID(), Kind: "generic", ConversationID: r.convID, Status: status, Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	old := aitask.NewToken()
	if err := st.SetToken(task.ID, old); err != nil {
		t.Fatal(err)
	}
	return task, old
}

// turn runs one follow-up turn with operator as the answer of the operator check.
func (r *taskRig) turn(t *testing.T, id string, operator error) {
	t.Helper()
	t.Setenv(monomind.EnvOverride, r.bin)
	cfg := testDeps(&testConfig{DBPath: r.dbPath, ProfileID: "default"})
	cfg.RequireOperator = func(string) error { return operator }
	cmd := newChatCmd(cfg)
	cmd.SetArgs([]string{"--conversation", r.convID, "--turn", id, "--", "hello"})
	var err error
	captureStdout(t, func() { err = cmd.Execute() })
	if err != nil {
		t.Fatalf("turn %s: %v", id, err)
	}
}

var envLine = regexp.MustCompile(`ENV \[(.*)\] \[(.*)\]`)

func (r *taskRig) env(t *testing.T) (id, token string) {
	t.Helper()
	b, _ := os.ReadFile(r.envLog)
	m := envLine.FindStringSubmatch(string(b))
	if m == nil {
		t.Fatalf("the fake monomind logged no turn: %q", b)
	}
	return m[1], m[2]
}

func TestOperatorFollowUpOnAnOpenTaskGetsAFreshTokenAndCanComplete(t *testing.T) {
	r := newTaskRig(t)
	task, old := r.task(t, "default", aitask.StatusNeedsUser)
	r.turn(t, "t1", nil)
	id, token := r.env(t)
	if id != task.ID || len(token) != 64 || token == old {
		t.Fatalf("turn env = %q %q (old token %q)", id, token, old)
	}
	st, done := r.store(t, "default")
	defer done()
	if st.CheckToken(task.ID, old) {
		t.Fatal("the old token still works after the rebind")
	}
	if !st.CheckToken(task.ID, token) {
		t.Fatal("the new token does not work")
	}
	// The turn ended without complete: the task needs the user again.
	if got, _ := st.Get(task.ID); got.Status != aitask.StatusNeedsUser {
		t.Fatalf("status after the turn = %s", got.Status)
	}
	svc := &aitask.Service{Store: st}
	if done, err := svc.Complete(task.ID, token, []byte(`{"summary":"answered"}`)); err != nil || done.Status != aitask.StatusComplete {
		t.Fatalf("complete with the follow-up's token: %+v %v", done, err)
	}
}

func TestFollowUpWhileRunningRebindsToo(t *testing.T) {
	r := newTaskRig(t)
	task, old := r.task(t, "default", aitask.StatusRunning)
	r.turn(t, "t1", nil)
	if id, _ := r.env(t); id != task.ID {
		t.Fatalf("env id = %q", id)
	}
	st, done := r.store(t, "default")
	defer done()
	if st.CheckToken(task.ID, old) {
		t.Fatal("the running turn's token survived the rebind")
	}
}

func TestFollowUpGetsNothingFromAnAgent(t *testing.T) {
	r := newTaskRig(t)
	task, old := r.task(t, "default", aitask.StatusNeedsUser)
	r.turn(t, "t1", errors.New("an agent is running this"))
	if id, token := r.env(t); id != "" || token != "" {
		t.Fatalf("an agent-run follow-up got the task env: %q %q", id, token)
	}
	st, done := r.store(t, "default")
	defer done()
	if !st.CheckToken(task.ID, old) {
		t.Fatal("an agent-run follow-up replaced the token")
	}
	if got, _ := st.Get(task.ID); got.Status != aitask.StatusNeedsUser {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestFollowUpGetsNothingForTerminalQueuedOrOtherProfileTasks(t *testing.T) {
	for name, tc := range map[string]struct{ profile, status string }{
		"complete":      {"default", aitask.StatusComplete},
		"failed":        {"default", aitask.StatusFailed},
		"cancelled":     {"default", aitask.StatusCancelled},
		"queued":        {"default", aitask.StatusQueued},
		"other profile": {"someone-else", aitask.StatusNeedsUser},
	} {
		r := newTaskRig(t)
		task, old := r.task(t, tc.profile, tc.status)
		r.turn(t, "t1", nil)
		if id, token := r.env(t); id != "" || token != "" {
			t.Errorf("%s: the turn got the task env %q %q", name, id, token)
		}
		st, done := r.store(t, tc.profile)
		if !st.CheckToken(task.ID, old) {
			t.Errorf("%s: the token was replaced", name)
		}
		if got, _ := st.Get(task.ID); got.Status != tc.status {
			t.Errorf("%s: status %s -> %s", name, tc.status, got.Status)
		}
		done()
	}
}
