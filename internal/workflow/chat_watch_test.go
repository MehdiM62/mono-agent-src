package workflow

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// watchState is one read of a run: its status and whether it is in flight.
type watchState struct {
	status   string
	inFlight bool
}

// scriptedExecs is a WebhookExecutions whose run goes through states, one
// per read; the last one stays. It records when each read came.
type scriptedExecs struct {
	mu     sync.Mutex
	states []watchState
	reads  []time.Time
	exec   *WorkflowExecution
	wf     *Workflow
}

func (f *scriptedExecs) ExecutionInFlight(context.Context, string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.states[min(len(f.reads), len(f.states)-1)]
	f.reads = append(f.reads, time.Now())
	return st.status, st.inFlight, nil
}

func (f *scriptedExecs) GetExecutionStatus(ctx context.Context, id string) (string, error) {
	st, _, err := f.ExecutionInFlight(ctx, id)
	return st, err
}

func (f *scriptedExecs) GetExecution(context.Context, string) (*WorkflowExecution, error) {
	return f.exec, nil
}

func (f *scriptedExecs) GetWorkflow(context.Context, string) (*Workflow, error) {
	if f.wf == nil {
		return nil, ErrWorkflowNotFound
	}
	return f.wf, nil
}

// chatWatch serves a lastNode chat at "watch", polled every 5ms, with a
// watch limit of limit.
func chatWatch(t *testing.T, limit time.Duration) (*WebhookServer, *chatRoute) {
	t.Helper()
	oldPoll, oldLimit := webhookPollInterval, chatWatchLimit
	webhookPollInterval, chatWatchLimit = 5*time.Millisecond, limit
	t.Cleanup(func() { webhookPollInterval, chatWatchLimit = oldPoll, oldLimit })
	s := NewWebhookServer(":0", zerolog.Nop())
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	route := &chatRoute{WorkflowID: "wf-1", NodeID: "t1", Path: "watch", ResponseMode: ChatRespondLastNode}
	if err := s.RegisterChat(route); err != nil {
		t.Fatal(err)
	}
	return s, route
}

// watchInBackground runs watchChatRun and returns a channel closed when it
// returns.
func watchInBackground(s *WebhookServer, route *chatRoute, execs WebhookExecutions, session string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.watchChatRun(route, execs, session, "exec-1", false)
	}()
	return done
}

func botMessages(session string) []string {
	var out []string
	for _, m := range ChatMessagesAfter(context.Background(), session, 0, 0) {
		if m.Role == "bot" {
			out = append(out, m.Text)
		}
	}
	return out
}

// A run that never ends, running or paused on a sub-workflow that never
// finishes (a parent left WAITING by a daemon restart), is watched until
// the limit, not forever; the chat says the run is still going. It used to
// be read ten times a second for as long as the process lived.
func TestChatWatchGivesUpAtItsLimit(t *testing.T) {
	for name, st := range map[string]watchState{
		"running":                      {"RUNNING", true},
		"waiting for its sub-workflow": {"WAITING", true},
	} {
		t.Run(name, func(t *testing.T) {
			s, route := chatWatch(t, 60*time.Millisecond)
			session := "watch-limit-" + st.status
			select {
			case <-watchInBackground(s, route, &scriptedExecs{states: []watchState{st}}, session):
			case <-time.After(5 * time.Second):
				t.Fatal("the watch went on past its limit")
			}
			if got := botMessages(session); len(got) != 1 || got[0] != chatStillRunning {
				t.Fatalf("bot said %q, want only %q", got, chatStillRunning)
			}
		})
	}
}

// The watch ends with the server and with the chat (its workflow
// deactivated), whatever the run does, and says nothing: no page reads it.
func TestChatWatchEndsWithTheServerAndTheChat(t *testing.T) {
	for _, c := range []struct {
		session string
		end     func(*WebhookServer)
	}{
		{"watch-end-server", func(s *WebhookServer) { _ = s.Stop(context.Background()) }},
		{"watch-end-chat", func(s *WebhookServer) { s.DeregisterChat("watch") }},
	} {
		t.Run(c.session, func(t *testing.T) {
			s, route := chatWatch(t, time.Hour)
			execs := &scriptedExecs{states: []watchState{{"WAITING", true}}}
			done := watchInBackground(s, route, execs, c.session)
			time.Sleep(30 * time.Millisecond)
			c.end(s)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the watch went on")
			}
			if got := botMessages(c.session); len(got) != 0 {
				t.Fatalf("bot said %q", got)
			}
		})
	}
}

// A run that waits only for its sub-workflows is watched on, read less
// often the longer it waits, and its answer reaches the chat when it ends.
// (The fake of webhook_run_test.go never reports such a wait.)
func TestChatWatchWaitsThroughSubWorkflowsWithBackoff(t *testing.T) {
	s, route := chatWatch(t, time.Minute)
	waiting := watchState{"WAITING", true}
	execs := &scriptedExecs{
		states: []watchState{{"RUNNING", true}, waiting, waiting, waiting, waiting, {"SUCCESS", false}},
		exec: &WorkflowExecution{Nodes: []WorkflowExecutionNode{
			{NodeID: "n1", Status: "SUCCESS", OutputItems: []Item{{JSON: map[string]interface{}{"output": "done"}}}},
		}},
	}
	session := "watch-backoff"
	select {
	case <-watchInBackground(s, route, execs, session):
	case <-time.After(5 * time.Second):
		t.Fatal("the watch did not end")
	}
	if got := botMessages(session); len(got) != 1 || got[0] != "done" {
		t.Fatalf("bot said %q, want only %q", got, "done")
	}
	execs.mu.Lock()
	reads := execs.reads
	execs.mu.Unlock()
	if len(reads) != 6 {
		t.Fatalf("%d reads, want 6", len(reads))
	}
	// After the k-th WAITING read (reads[k], k = 1..4) the pause is
	// 5ms << k: 10, 20, 40, 80ms.
	for k := 1; k <= 4; k++ {
		want := webhookPollInterval << k
		if gap := reads[k+1].Sub(reads[k]); gap < want {
			t.Errorf("pause after WAITING read %d = %v, want at least %v", k, gap, want)
		}
	}
}

// The watch's limit is chatWatchLimit, or the workflow's executionTimeout
// when that is longer.
func TestChatWatchForTakesTheLongerLimit(t *testing.T) {
	oldLimit := chatWatchLimit
	chatWatchLimit = 30 * time.Minute
	t.Cleanup(func() { chatWatchLimit = oldLimit })
	for _, c := range []struct {
		wf   *Workflow
		want time.Duration
	}{
		{nil, 30 * time.Minute},
		{&Workflow{}, 30 * time.Minute},
		{&Workflow{Settings: map[string]interface{}{ExecutionTimeoutSetting: 60.0}}, 30 * time.Minute},
		{&Workflow{Settings: map[string]interface{}{ExecutionTimeoutSetting: 7200.0}}, 2 * time.Hour},
	} {
		if got := chatWatchFor(context.Background(), &scriptedExecs{wf: c.wf}, "wf-1"); got != c.want {
			t.Errorf("chatWatchFor(%v) = %v, want %v", c.wf, got, c.want)
		}
	}
}
