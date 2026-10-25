package dynorg

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/tasks"
)

func usageCost(usd float64) monomind.Event {
	return monomind.Event{Type: monomind.EventUsage, HasCostUSD: true, CostUSD: usd}
}

func spendNear(a, b float64) bool { d := a - b; return d > -0.0001 && d < 0.0001 }

// A worker that streams its cost is charged to the session while it runs: the 90 percent
// warning and the 100 percent halt fire before it finishes.
func TestMissionSpendIsRecordedWhileTheWorkerRuns(t *testing.T) {
	r := newRig(t, rigOpts{budget: 10})
	a := r.add(spec{title: "a", files: "a.go"})
	r.approve()
	step := make(chan struct{})
	r.ex.onRun = func(ctx context.Context, id int64, on func(monomind.Event)) {
		on(usageCost(9.2))
		select {
		case <-step:
		case <-ctx.Done():
			return
		}
		on(usageCost(10))
		<-ctx.Done()
	}
	r.next(r.c)

	eventually(t, "the 90 percent warning mid-worker", func() bool { return countNotices(r.em, "mission.budget_90") == 1 })
	if got := r.session().SpentUSD; !spendNear(got, 9.2) || r.session().State != tasks.SessionRunning {
		t.Fatalf("mid-worker: spent %v, state %s", got, r.session().State)
	}
	if atomic.LoadInt32(&r.ex.running) != 1 {
		t.Fatal("the worker should still be running")
	}

	close(step)
	eventually(t, "the halt mid-worker", func() bool { return r.session().State == tasks.SessionPaused && atomic.LoadInt32(&r.ex.running) == 0 })
	if got := r.session().SpentUSD; !spendNear(got, 10) {
		t.Fatalf("spent %v after the halt: the worker's cost is counted once", got)
	}
	if n := countNotices(r.em, "mission.budget_halt"); n != 1 {
		t.Fatalf("halt notices %d", n)
	}
	r.waitStatus(a.ID, tasks.StatusReady)
	if n := countNotices(r.em, "mission.budget_90"); n != 1 {
		t.Fatalf("warnings %d", n)
	}
}

// What a worker streamed and what it reports at the end are one cost, not two.
func TestMissionStreamedCostIsNotCountedAgainAtTheEnd(t *testing.T) {
	r := newRig(t, rigOpts{budget: 10})
	a := r.add(spec{title: "a", files: "a.go"})
	r.approve()
	r.ex.cost = 0.4 // the result's cost
	r.ex.onRun = func(ctx context.Context, id int64, on func(monomind.Event)) { on(usageCost(0.4)) }
	r.next(r.c)
	eventually(t, "the streamed spend", func() bool { return spendNear(r.session().SpentUSD, 0.4) })
	r.ex.release(a.ID)
	r.waitStatus(a.ID, tasks.StatusReview)
	eventually(t, "the worker to end", func() bool { return r.c.taskWorker(a.ID) == nil })
	if got := r.session().SpentUSD; !spendNear(got, 0.4) {
		t.Fatalf("spent %v, want 0.4 once", got)
	}
}

// The watcher applies the 90 percent warning to a running worker on its own tick.
func TestMissionTickWarnsAtNinetyPercentWhileAWorkerRuns(t *testing.T) {
	r := newRig(t, rigOpts{budget: 10})
	r.add(spec{title: "a", files: "a.go"})
	r.approve()
	r.next(r.c)
	if _, err := r.store.AddSessionSpend(tbg, "default", r.sid, 9.5, "other spend", operator); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the warning from the tick", func() bool { return countNotices(r.em, "mission.budget_90") == 1 })
}
