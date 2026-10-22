package applypark

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Runner is what the queue needs from the application commands: start one
// fill, wait for its task, record its result. Each fill is still the confined
// application-apply task; the queue only decides when to start it.
type Runner interface {
	// Start submits the application's fill task and returns the task id.
	Start(ctx context.Context, appID string) (taskID string, err error)
	// Wait blocks until the task is over (complete, failed, cancelled or
	// needs_user) and returns its final status.
	Wait(ctx context.Context, taskID string) (status string, err error)
	// Finalize records the finished task on the application (fill record,
	// awaiting_submit or needs_you, the tab binding). A task that ended
	// needs_user, with no result, becomes needs_you with a reason.
	Finalize(ctx context.Context, appID string) error
	// Sweep releases the slots of tabs the person closed or finished with.
	Sweep(ctx context.Context) error
}

// Outcome of one application in the queue.
type Outcome struct {
	ApplicationID string `json:"applicationId"`
	TaskID        string `json:"taskId,omitempty"`
	// Result: parked (filled, tab left open), needs_you, failed or not_started.
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

// Outcomes of a fill.
const (
	ResultParked     = "parked"
	ResultNeedsYou   = "needs_you"
	ResultFailed     = "failed"
	ResultNotStarted = "not_started"
)

// Queue starts fills one after another, never more than Max at once counting
// the tabs already waiting on the person (parked) plus the fills running.
// It is a foreground loop: it ends when every application has been tried, or
// when ctx ends (then the applications not yet started are reported
// not_started). It never runs unattended.
type Queue struct {
	Store  Store
	Max    int
	Runner Runner
	// Tick is how often a full queue looks again for a freed slot.
	Tick time.Duration
	// Log, when set, hears one line per step.
	Log func(format string, a ...any)
	// Status reads the application's status after a fill, to tell parked from needs_you.
	Status func(ctx context.Context, appID string) (string, error)
}

func (q Queue) logf(format string, a ...any) {
	if q.Log != nil {
		q.Log(format, a...)
	}
}

// Run works through ids in order and reports an Outcome for each.
func (q Queue) Run(ctx context.Context, ids []string) ([]Outcome, error) {
	max := ClampMax(q.Max)
	tick := q.Tick
	if tick <= 0 {
		tick = 5 * time.Second
	}
	var (
		mu       sync.Mutex
		running  = map[string]bool{}
		outcomes = map[string]Outcome{}
		wg       sync.WaitGroup
		wake     = make(chan struct{}, 1)
		// write serialises what writes the database: SQLite refuses a
		// transaction that reads and then writes while another writer commits.
		write sync.Mutex
	)
	signal := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	finish := func(o Outcome) {
		mu.Lock()
		outcomes[o.ApplicationID] = o
		delete(running, o.ApplicationID)
		mu.Unlock()
		signal()
	}
	runOne := func(id string) {
		defer wg.Done()
		o := Outcome{ApplicationID: id, Result: ResultFailed}
		write.Lock()
		taskID, err := q.Runner.Start(ctx, id)
		write.Unlock()
		if err != nil {
			o.Error = err.Error()
			finish(o)
			return
		}
		o.TaskID = taskID
		status, err := q.Runner.Wait(ctx, taskID)
		if err != nil {
			o.Error = err.Error()
			finish(o)
			return
		}
		if status != "complete" && status != "needs_user" {
			o.Error = "task " + status
			finish(o)
			return
		}
		write.Lock()
		err = q.Runner.Finalize(ctx, id)
		write.Unlock()
		if err != nil {
			o.Error = err.Error()
			finish(o)
			return
		}
		o.Result = ResultNeedsYou
		if q.Status != nil {
			if st, err := q.Status(ctx, id); err == nil && st == "awaiting_submit" {
				o.Result = ResultParked
			}
		}
		finish(o)
	}

	next := 0
	for {
		if ctx.Err() == nil && next < len(ids) {
			occupied, err := q.Store.Occupied(ctx)
			if err != nil {
				wg.Wait()
				return q.report(ids, outcomes), err
			}
			mu.Lock()
			busy := 0
			for id := range running {
				// A running fill whose tab is already bound is counted once, as a tab.
				if t, _ := q.Store.Get(ctx, id); t == nil || (t.State != StateParked && t.State != StateActive) {
					busy++
				}
			}
			for next < len(ids) && occupied+busy < max {
				id := ids[next]
				next++
				if _, dup := running[id]; dup {
					continue
				}
				running[id] = true
				busy++
				wg.Add(1)
				q.logf("starting %s (%d of %d slots in use)", id, occupied+busy, max)
				go runOne(id)
			}
			mu.Unlock()
		}
		mu.Lock()
		idle := len(running) == 0
		mu.Unlock()
		if idle && (next >= len(ids) || ctx.Err() != nil) {
			break
		}
		if ctx.Err() == nil && next < len(ids) {
			write.Lock()
			err := q.Runner.Sweep(ctx)
			write.Unlock()
			if err != nil {
				q.logf("sweep: %v", err)
			}
		}
		select {
		case <-wake:
		case <-time.After(tick):
		case <-ctx.Done():
			// the fills in flight see the same ctx and return.
			wg.Wait()
		}
	}
	wg.Wait()
	return q.report(ids, outcomes), ctx.Err()
}

func (q Queue) report(ids []string, outcomes map[string]Outcome) []Outcome {
	out := make([]Outcome, 0, len(ids))
	for _, id := range ids {
		if o, ok := outcomes[id]; ok {
			out = append(out, o)
		} else {
			out = append(out, Outcome{ApplicationID: id, Result: ResultNotStarted, Error: fmt.Sprintf("not started: %s", "the queue ended first")})
		}
	}
	return out
}
