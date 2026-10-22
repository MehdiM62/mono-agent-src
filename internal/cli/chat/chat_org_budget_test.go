package chat

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/monomind"
)

// workersRig has an open task with a $1 cap on the rig's conversation.
func workersRig(t *testing.T) (*taskRig, *aitask.Store, *Deps) {
	t.Helper()
	r := newTaskRig(t)
	task, _ := r.task(t, "default", aitask.StatusRunning)
	st, done := r.store(t, "default")
	t.Cleanup(done)
	if _, err := st.DB.Exec(`UPDATE ai_tasks SET budget_usd = 1 WHERE id = ?`, task.ID); err != nil {
		t.Fatal(err)
	}
	return r, st, testDeps(&testConfig{DBPath: r.dbPath, ProfileID: "default"})
}

// A task's workers share the turn's cap with the lead: the pool is half of it
// (or the org budget when lower), the lead keeps the rest, and an org budget of
// 0 is not "no limit".
func TestTaskWorkersGetAShareOfTheTurnsCapNeverUnlimited(t *testing.T) {
	r, st, cfg := workersRig(t)
	opts := monomind.ExecOptions{BudgetUSD: 1}
	pool, exec, ok, _ := taskWorkers(st.DB, cfg, r.convID, "turn-1", coderSettings{OrgBudgetUSD: 0}, &opts, nil)
	if !ok || exec == nil || pool != 0.5 || opts.BudgetUSD != 0.5 {
		t.Fatalf("org budget 0: pool %v, lead %v, ok %v", pool, opts.BudgetUSD, ok)
	}
	opts = monomind.ExecOptions{BudgetUSD: 1}
	if pool, _, _, _ := taskWorkers(st.DB, cfg, r.convID, "turn-1", coderSettings{OrgBudgetUSD: 0.2}, &opts, nil); pool != 0.2 || opts.BudgetUSD != 0.8 {
		t.Fatalf("org budget 0.2: pool %v, lead %v", pool, opts.BudgetUSD)
	}
	opts = monomind.ExecOptions{BudgetUSD: 1}
	if pool, _, _, _ := taskWorkers(st.DB, cfg, r.convID, "turn-1", coderSettings{OrgBudgetUSD: 9}, &opts, nil); pool != 0.5 {
		t.Fatalf("an org budget above the share raised the pool to %v", pool)
	}
	// No cap on the turn: no workers.
	opts = monomind.ExecOptions{}
	if _, exec, ok, why := taskWorkers(st.DB, cfg, r.convID, "turn-1", coderSettings{}, &opts, nil); !ok || exec != nil || why == "" {
		t.Fatalf("a task turn with no cap got workers: %v %v %q", ok, exec != nil, why)
	}
	// A conversation with no open task is not touched.
	opts = monomind.ExecOptions{BudgetUSD: 1}
	if _, _, ok, _ := taskWorkers(st.DB, cfg, "some-other-conversation", "t", coderSettings{}, &opts, nil); ok || opts.BudgetUSD != 1 {
		t.Fatalf("a non-task conversation was changed: ok %v, budget %v", ok, opts.BudgetUSD)
	}
}

// Workers that run together can't reserve more than the pool: each run reserves
// before it starts, and is capped at what it was given. Without the reservation
// each would run with the whole remainder.
func TestTaskWorkersTogetherCannotExceedThePool(t *testing.T) {
	r, st, cfg := workersRig(t)
	opts := monomind.ExecOptions{BudgetUSD: 1}
	var granted, ran atomic.Int64 // in micro-USD
	inner := func(_ context.Context, o monomind.ExecOptions, _ func(monomind.Event)) (*monomind.TurnResult, error) {
		granted.Add(int64(o.BudgetUSD * 1e6))
		ran.Add(1)
		return &monomind.TurnResult{}, nil // reports no cost: counts for what it reserved
	}
	_, exec, _, _ := taskWorkers(st.DB, cfg, r.convID, "turn-1", coderSettings{}, &opts, inner)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = exec(context.Background(), monomind.ExecOptions{Runtime: "claude", BudgetUSD: 0.3}, nil)
		}()
	}
	wg.Wait()
	if g := float64(granted.Load()) / 1e6; g > 0.5+1e-6 || ran.Load() < 1 {
		t.Fatalf("workers were granted %v USD in total over %d runs, pool is 0.50", g, ran.Load())
	}
	// The pool is spent: the next worker is refused with the hint, and never runs.
	before := ran.Load()
	_, err := exec(context.Background(), monomind.ExecOptions{Runtime: "claude", BudgetUSD: 0.1}, nil)
	if err == nil || !strings.Contains(err.Error(), "Settings > AI task budgets") || ran.Load() != before {
		t.Fatalf("a worker on a spent pool: %v (ran %d, was %d)", err, ran.Load(), before)
	}
	// A runtime that can't hold a cap can't be a worker.
	if _, err := exec(context.Background(), monomind.ExecOptions{Runtime: "kilo", BudgetUSD: 0.1}, nil); err == nil || ran.Load() != before {
		t.Fatalf("kilo ran as a worker: %v", err)
	}
}
