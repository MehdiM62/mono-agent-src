package workflow

import (
	"context"
	"sync"
	"testing"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/account/accounttest"
)

// countingSource is a trigger source that counts how often it is armed and disarmed: an SSE stream,
// a queue consumer or a file watcher would start working at Activate.
type countingSource struct {
	mu              sync.Mutex
	armed, disarmed int
}

func (s *countingSource) Activate(*Workflow, *WorkflowNode, func([]Item)) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.armed++
	return func() { s.mu.Lock(); s.disarmed++; s.mu.Unlock() }, nil
}

func (s *countingSource) counts() (armed, disarmed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.armed, s.disarmed
}

// A locked daemon arms no trigger: not at restore, not at Start, not in the reconcile loop. A
// valid login arms the active workflows at the next reconcile, and a lock disarms them at the
// next one.
func TestLockedAccountArmsNoTrigger(t *testing.T) {
	signedIn := accounttest.Install(t, accounttest.SignedIn)
	lock := func() { account.InstallForTest(t, nil) } // strict, the date behind the clock, no guard: refused
	unlock := func() { account.InstallForTest(t, signedIn) }

	store := newFullEngineStore(t)
	ctx := context.Background()
	saveTriggerWorkflow(t, ctx, store, "wf", scheduleNode("s", "0 0 9 * * *", ""),
		WorkflowNode{ID: "q", Type: "trigger.fake", Name: "q", Config: map[string]interface{}{}})
	if err := store.SetWorkflowActive(ctx, "wf", true); err != nil {
		t.Fatal(err)
	}
	eng, sched := newDaemonEngine(t, store)
	src := &countingSource{}
	eng.RegisterTriggerSource("trigger.fake", src)

	lock()
	if err := eng.reregisterTriggers(ctx); err != nil {
		t.Fatal(err)
	}
	if err := eng.RestoreActiveWorkflows(ctx); err != nil {
		t.Fatal(err)
	}
	eng.reconcileTriggers(ctx)
	if armed, _ := src.counts(); armed != 0 || len(sched.specs()) != 0 {
		t.Fatalf("locked: %d sources armed, schedules %v; want none", armed, sched.specs())
	}

	unlock()
	eng.reconcileTriggers(ctx)
	if armed, _ := src.counts(); armed != 1 || len(sched.specs()) != 1 {
		t.Fatalf("signed in: %d sources armed, schedules %v; want both triggers armed", armed, sched.specs())
	}

	lock()
	eng.reconcileTriggers(ctx)
	if _, disarmed := src.counts(); disarmed != 1 || len(sched.specs()) != 0 {
		t.Fatalf("locked again: %d sources disarmed, schedules %v; want everything disarmed", disarmed, sched.specs())
	}
	eng.reconcileTriggers(ctx)
	if armed, _ := src.counts(); armed != 1 {
		t.Fatalf("a locked reconcile armed a source again (%d)", armed)
	}

	unlock()
	eng.reconcileTriggers(ctx)
	if armed, _ := src.counts(); armed != 2 || len(sched.specs()) != 1 {
		t.Fatalf("signed in again: %d arms, schedules %v; want the triggers armed again", armed, sched.specs())
	}
}

// listCountingStore counts the workflow listings, the first thing the prune pass does.
type listCountingStore struct {
	*engineGateStore
	lists int
}

func (s *listCountingStore) ListWorkflows(ctx context.Context, profileID string) ([]Workflow, error) {
	s.lists++
	return s.engineGateStore.ListWorkflows(ctx, profileID)
}

// A locked account prunes no execution history.
func TestPruneGate(t *testing.T) {
	eachEngineGateMode(t, func(t *testing.T, refused bool) {
		store := &listCountingStore{engineGateStore: newEngineGateStore(nil)}
		newTriggerEngine(store).runPrune(context.Background())
		if ran := store.lists > 0; ran == refused {
			t.Fatalf("prune ran = %v, want %v", ran, !refused)
		}
	})
}
