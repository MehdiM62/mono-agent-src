package workflow

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
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
// per read; the last one stays. It records when each read came. With
// holdEnd, a read that finds the run ended comes back only once the
// watch's limit has passed, as a read that is slow at the limit does.
type scriptedExecs struct {
	mu      sync.Mutex
	states  []watchState
	reads   []time.Time
	exec    *WorkflowExecution
	wf      *Workflow
	holdEnd bool
}

func (f *scriptedExecs) ExecutionInFlight(ctx context.Context, _ string) (string, bool, error) {
	f.mu.Lock()
	st := f.states[min(len(f.reads), len(f.states)-1)]
	f.reads = append(f.reads, time.Now())
	f.mu.Unlock()
	if f.holdEnd && !st.inFlight {
		<-ctx.Done()
	}
	return st.status, st.inFlight, nil
}

func (f *scriptedExecs) GetExecutionStatus(ctx context.Context, id string) (string, error) {
	st, _, err := f.ExecutionInFlight(ctx, id)
	return st, err
}

func (f *scriptedExecs) GetExecution(ctx context.Context, _ string) (*WorkflowExecution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.exec, nil
}

func (f *scriptedExecs) GetWorkflow(context.Context, string) (*Workflow, error) {
	if f.wf == nil {
		return nil, ErrWorkflowNotFound
	}
	return f.wf, nil
}

// doneExec is a run whose last node answered "done".
var doneExec = &WorkflowExecution{Nodes: []WorkflowExecutionNode{
	{NodeID: "n1", Status: "SUCCESS", OutputItems: []Item{{JSON: map[string]interface{}{"output": "done"}}}},
}}

var watchSessions atomic.Int64

// watchSession is a session id no earlier run of the test used: the chat
// messages live in a store of the process, so a fixed id would find the
// messages of the previous iteration under -count.
func watchSession(base string) string {
	return fmt.Sprintf("%s-%d", base, watchSessions.Add(1))
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

// heldPauses makes the watch's pauses the test's: a pause starting sends
// on paused, and resume(t) ends it, once the watch is waiting on it.
func heldPauses(t *testing.T) (paused <-chan struct{}, resume func(*testing.T)) {
	t.Helper()
	entered, tick := make(chan struct{}, 1), make(chan time.Time)
	old := chatWatchPause
	chatWatchPause = func(time.Duration) <-chan time.Time { entered <- struct{}{}; return tick }
	t.Cleanup(func() { chatWatchPause = old })
	return entered, func(t *testing.T) {
		t.Helper()
		select {
		case tick <- time.Now():
		case <-time.After(5 * time.Second):
			t.Fatal("the watch is not pausing")
		}
	}
}

// awaitPause fails the test unless the watch starts a pause within 5s.
func awaitPause(t *testing.T, paused <-chan struct{}) {
	t.Helper()
	select {
	case <-paused:
	case <-time.After(5 * time.Second):
		t.Fatal("the watch did not pause")
	}
}

// awaitWatch fails the test unless the watch ends within 5s.
func awaitWatch(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the watch went on")
	}
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
			session := watchSession("watch-limit")
			awaitWatch(t, watchInBackground(s, route, &scriptedExecs{states: []watchState{st}}, session))
			if got := botMessages(session); len(got) != 1 || got[0] != chatStillRunning {
				t.Fatalf("bot said %q, want only %q", got, chatStillRunning)
			}
		})
	}
}

// A run that ends as the limit passes still gets its answer: the result is
// read on the server's context, not the expired watch's.
func TestChatWatchAnswersARunThatEndsAtTheLimit(t *testing.T) {
	s, route := chatWatch(t, 50*time.Millisecond)
	session := watchSession("watch-at-limit")
	execs := &scriptedExecs{states: []watchState{{"SUCCESS", false}}, exec: doneExec, holdEnd: true}
	awaitWatch(t, watchInBackground(s, route, execs, session))
	if got := botMessages(session); len(got) != 1 || got[0] != "done" {
		t.Fatalf("bot said %q, want only %q", got, "done")
	}
}

// The watch ends with the server, silently, and with the chat (its
// workflow deactivated, or re-registered by an edit), saying it stopped
// being served and the answer won't come.
func TestChatWatchEndsWithTheServerAndTheChat(t *testing.T) {
	for _, c := range []struct {
		name string
		end  func(*WebhookServer)
		want []string
	}{
		{"server stops", func(s *WebhookServer) { _ = s.Stop(context.Background()) }, nil},
		{"chat deregisters", func(s *WebhookServer) { s.DeregisterChat("watch") }, []string{chatNotServed}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, route := chatWatch(t, time.Hour)
			session := watchSession("watch-end")
			execs := &scriptedExecs{states: []watchState{{"WAITING", true}}}
			done := watchInBackground(s, route, execs, session)
			time.Sleep(30 * time.Millisecond)
			c.end(s)
			awaitWatch(t, done)
			if got := botMessages(session); !slices.Equal(got, c.want) {
				t.Fatalf("bot said %q, want %q", got, c.want)
			}
		})
	}
}

// As the engine stops it ends the watches, then deregisters the chats. A
// watch already past its pause then finds its chat gone with the watches
// ended, and says nothing: it is the engine that stopped.
func TestChatWatchSaysNothingWhenTheEngineStops(t *testing.T) {
	s, route := chatWatch(t, time.Hour)
	paused, resume := heldPauses(t)
	session := watchSession("watch-engine-stop")
	done := watchInBackground(s, route, &scriptedExecs{states: []watchState{{"WAITING", true}}}, session)
	awaitPause(t, paused)
	s.chats.mu.Lock() // holds the watch before it looks for its chat
	resume(t)
	// The watch is past its pause now.
	s.endWatches()
	delete(s.chats.m, route.Path) // DeregisterChat, under the lock held
	s.chats.mu.Unlock()
	awaitWatch(t, done)
	if got := botMessages(session); len(got) != 0 {
		t.Fatalf("bot said %q", got)
	}
}

// A chat deregistered and registered again at its path (an edit) before
// the watch looks is still served: the watch says nothing, goes on, and
// the run's answer reaches the chat.
func TestChatWatchGoesOnThroughAReRegister(t *testing.T) {
	s, route := chatWatch(t, time.Hour)
	paused, resume := heldPauses(t)
	session := watchSession("watch-reregister")
	execs := &scriptedExecs{states: []watchState{{"WAITING", true}, {"SUCCESS", false}}, exec: doneExec}
	done := watchInBackground(s, route, execs, session)
	awaitPause(t, paused)
	s.DeregisterChat(route.Path)
	if err := s.RegisterChat(&chatRoute{WorkflowID: "wf-1", NodeID: "t1", Path: route.Path, ResponseMode: ChatRespondLastNode}); err != nil {
		t.Fatal(err)
	}
	resume(t)
	awaitPause(t, paused) // read WAITING and paused again: still watching
	if got := botMessages(session); len(got) != 0 {
		t.Fatalf("bot said %q while the run waits", got)
	}
	resume(t)
	awaitWatch(t, done)
	if got := botMessages(session); len(got) != 1 || got[0] != "done" {
		t.Fatalf("bot said %q, want only %q", got, "done")
	}
}

// Engine.Stop ends the chat watches before it deregisters the chats (the
// order TestChatWatchSaysNothingWhenTheEngineStops relies on). The test
// holds the chats' lock, so a Stop that deregisters first waits for it with
// the watches still on.
func TestEngineStopEndsChatWatchesBeforeDeregisteringChats(t *testing.T) {
	eng := newTestEngine(t, newFullEngineStore(t), NewNodeTypeRegistry())
	node := &WorkflowNode{ID: "t", Type: TriggerNodeTypeChat, Config: map[string]interface{}{"path": "stopping"}}
	if err := eng.triggerMgr.activateChat("wf-chat", node); err != nil {
		t.Fatal(err)
	}
	s := eng.webhookServer
	s.chats.mu.RLock()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = eng.Stop()
	}()
	for deadline := time.Now().Add(2 * time.Second); s.watches.Err() == nil && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	ended := s.watches.Err() != nil
	s.chats.mu.RUnlock()
	<-stopped
	if !ended {
		t.Fatal("the chat watches were still on while the engine deregistered the chats")
	}
	if s.servesChat("stopping") {
		t.Fatal("the stopped engine still serves the chat")
	}
}

// A run that waits only for its sub-workflows is watched on, read less
// often the longer it waits, read at the base pace again once it runs, and
// its answer reaches the chat when it ends. (The fake of
// webhook_run_test.go never reports such a wait.)
func TestChatWatchWaitsThroughSubWorkflowsWithBackoff(t *testing.T) {
	var mu sync.Mutex
	var pauses []time.Duration
	old := chatWatchPause
	chatWatchPause = func(d time.Duration) <-chan time.Time {
		mu.Lock()
		pauses = append(pauses, d)
		mu.Unlock()
		return time.After(d)
	}
	t.Cleanup(func() { chatWatchPause = old })
	s, route := chatWatch(t, time.Minute)
	running, waiting := watchState{"RUNNING", true}, watchState{"WAITING", true}
	execs := &scriptedExecs{
		states: []watchState{running, waiting, waiting, waiting, running, waiting, {"SUCCESS", false}},
		exec:   doneExec,
	}
	session := watchSession("watch-backoff")
	awaitWatch(t, watchInBackground(s, route, execs, session))
	if got := botMessages(session); len(got) != 1 || got[0] != "done" {
		t.Fatalf("bot said %q, want only %q", got, "done")
	}
	// pauses[i] comes before read i: the base pace, doubling over the
	// WAITING reads, back to the base after the second RUNNING read.
	base := webhookPollInterval
	want := []time.Duration{base, base, 2 * base, 4 * base, 8 * base, base, 2 * base}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(pauses, want) {
		t.Fatalf("pauses = %v, want %v", pauses, want)
	}
	execs.mu.Lock()
	reads := execs.reads
	execs.mu.Unlock()
	if len(reads) != len(want) {
		t.Fatalf("%d reads, want %d", len(reads), len(want))
	}
	for i := 1; i < len(reads); i++ {
		if gap := reads[i].Sub(reads[i-1]); gap < want[i] {
			t.Errorf("read %d came %v after the one before, want at least %v", i, gap, want[i])
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
