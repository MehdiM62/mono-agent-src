package agentroster

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

// AutoScheduler runs automatic re-validation. Every dependency is a func so
// tests run it with a fake clock and a fake validator.
type AutoScheduler struct {
	DB  *sql.DB
	Now func() time.Time
	// Busy reports whether a chat turn, workflow run or org run is active,
	// with a short reason.
	Busy func(ctx context.Context) (bool, string)
	// Pick returns the next run (nil when nothing is stale). It runs
	// before the lock is taken: a scan must not block a manual validate.
	Pick func(ctx context.Context, maxModels int) (*AutoPlan, error)
	// Repick, when set, plans again once the lock is held, without a new
	// scan: a manual validation that just finished may have re-checked
	// some of the models. Nil keeps the first plan.
	Repick func(ctx context.Context, maxModels int) (*AutoPlan, error)
	// Validate runs the plan's tests, one at a time. Its ctx is cancelled
	// when the app gets busy or the setting is turned off mid-run.
	Validate func(ctx context.Context, p AutoPlan) (AutoRunResult, error)
	// Lock takes the validation lock `agent validate` also takes; ok false
	// means another validation runs.
	Lock func() (release func(), ok bool)
	// Interval between checks; 1 minute when zero.
	Interval time.Duration
	// WatchInterval is how often a run in progress re-checks Busy and the
	// setting; 5 seconds when zero.
	WatchInterval time.Duration
	// NothingBackoff is how long to wait after "nothing stale" before
	// planning (and scanning) again; 1 hour when zero.
	NothingBackoff time.Duration
	Logf           func(format string, args ...any)

	mu        sync.Mutex
	lastBusy  time.Time // start, last busy check, or last run: the quiet period counts from here
	planAfter time.Time // no planning before this (backoff after nothing stale)
	started   bool
}

// Check outcomes.
const (
	AutoCheckOff        = "off"
	AutoCheckBusy       = "busy"
	AutoCheckQuiet      = "waiting for quiet period"
	AutoCheckCap        = "daily cap reached"
	AutoCheckLocked     = "another validation is running"
	AutoCheckNothing    = "nothing stale"
	AutoCheckRan        = "ran"
	AutoCheckCancelled  = "cancelled"
	AutoCheckFailed     = "failed"
	autoDefaultInterval = time.Minute
	autoDefaultWatch    = 5 * time.Second
	autoDefaultBackoff  = time.Hour
)

// Why a run in progress was cancelled.
var (
	errAutoBusy      = errors.New("the app got busy")
	errAutoTurnedOff = errors.New("turned off")
)

// Start marks the scheduler as started now: the quiet period counts from
// here, so nothing runs at startup. Run calls it; tests call it directly.
func (s *AutoScheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		s.started, s.lastBusy = true, s.Now()
	}
}

// Run checks every Interval until ctx ends. A run in progress is cancelled
// with ctx; Run returns once it has stopped and its results are stored.
func (s *AutoScheduler) Run(ctx context.Context) {
	s.Start()
	interval := s.Interval
	if interval <= 0 {
		interval = autoDefaultInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Step(ctx)
		}
	}
}

// Step makes one decision and, when everything allows it, runs one
// runtime's stale models. It returns the outcome (one of the AutoCheck
// values, with a reason appended for busy, cancelled and failed).
func (s *AutoScheduler) Step(ctx context.Context) string {
	s.Start()
	now := s.Now()
	cfg, err := LoadAutoConfig(ctx, s.DB)
	if err != nil {
		s.logf("auto re-validation: %v", err)
		s.markBusy(now)
		return AutoCheckFailed
	}
	if !cfg.Enabled {
		// Turning it on later still waits a full quiet period.
		s.markBusy(now)
		return s.record(ctx, now, AutoCheckOff, time.Time{})
	}
	if busy, why := s.Busy(ctx); busy {
		s.markBusy(now)
		return s.record(ctx, now, withReason(AutoCheckBusy, why), time.Time{})
	}
	s.mu.Lock()
	quietUntil, planAfter := s.lastBusy.Add(cfg.QuietPeriod), s.planAfter
	s.mu.Unlock()
	if now.Before(quietUntil) {
		return s.record(ctx, now, AutoCheckQuiet, quietUntil)
	}
	if now.Before(planAfter) {
		return s.record(ctx, now, AutoCheckNothing, planAfter)
	}
	// A state that can't be read must not be taken for "nothing ran
	// today": the cap would fail open, and saving it would reset the count.
	st, err := LoadAutoState(ctx, s.DB, now)
	if err != nil {
		s.logf("auto re-validation: reading state: %v", err)
		s.markBusy(now)
		return AutoCheckFailed
	}
	if st.RuntimesToday >= cfg.MaxRuntimesPerDay {
		return s.record(ctx, now, AutoCheckCap, nextDay(now).Add(cfg.QuietPeriod))
	}

	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stopWatch := s.watch(runCtx, cancel)
	defer stopWatch()

	plan, err := s.Pick(runCtx, cfg.MaxModelsPerRun)
	if cause := context.Cause(runCtx); cause != nil && ctx.Err() == nil {
		s.markBusy(s.Now())
		return s.record(ctx, now, withReason(AutoCheckCancelled, cause.Error()), time.Time{})
	}
	if err != nil {
		s.logf("auto re-validation: planning: %v", err)
		s.markBusy(now)
		return s.record(ctx, now, withReason(AutoCheckFailed, err.Error()), time.Time{})
	}
	if plan == nil || len(plan.Targets) == 0 {
		backoff := s.NothingBackoff
		if backoff <= 0 {
			backoff = autoDefaultBackoff
		}
		next := now.Add(max(backoff, cfg.QuietPeriod))
		s.mu.Lock()
		s.planAfter = next
		s.mu.Unlock()
		return s.record(ctx, now, AutoCheckNothing, next)
	}
	release, ok := s.Lock()
	if !ok {
		s.markBusy(now)
		return s.record(ctx, now, AutoCheckLocked, time.Time{})
	}
	defer release()
	if s.Repick != nil {
		plan, err = s.Repick(runCtx, cfg.MaxModelsPerRun)
		if cause := context.Cause(runCtx); cause != nil && ctx.Err() == nil {
			s.markBusy(s.Now())
			return s.record(ctx, now, withReason(AutoCheckCancelled, cause.Error()), time.Time{})
		}
		if err != nil {
			s.logf("auto re-validation: planning: %v", err)
			s.markBusy(now)
			return s.record(ctx, now, withReason(AutoCheckFailed, err.Error()), time.Time{})
		}
		if plan == nil || len(plan.Targets) == 0 {
			return s.record(ctx, now, AutoCheckNothing, time.Time{})
		}
	}

	// The runtime counts against today's cap before the calls are made, so
	// a crash mid-run can never lead to more runs than the cap.
	st.RuntimesToday++
	st.LastRunAt, st.LastRuntime, st.LastSummary, st.LastError = now, plan.Runtime, nil, ""
	if err := SaveAutoState(ctx, s.DB, st); err != nil {
		s.logf("auto re-validation: saving state: %v", err)
		return AutoCheckFailed
	}
	s.logf("auto re-validation: testing %d stale model(s) of %s (≈ $%.4f, %d with unknown cost)",
		len(plan.Targets), plan.Runtime, plan.EstCostUSD, plan.UnknownCost)
	res, runErr := s.Validate(runCtx, *plan)
	stopWatch()
	end := s.Now()
	s.markBusy(end)

	out := AutoCheckRan
	switch cause := context.Cause(runCtx); {
	case cause != nil && ctx.Err() == nil:
		out = withReason(AutoCheckCancelled, cause.Error())
	case runErr != nil:
		out = withReason(AutoCheckFailed, runErr.Error())
	}
	saveCtx := context.WithoutCancel(ctx)
	st, err = LoadAutoState(saveCtx, s.DB, end)
	if err != nil {
		// Never save over a state that couldn't be read: the count would
		// go back to zero.
		s.logf("auto re-validation: reading state after the run: %v", err)
		return out
	}
	sum := res.Summary
	st.LastSummary = &sum
	st.SpentTodayUSD += res.SpentUSD
	st.UnknownCostCall += res.UnknownCost
	if runErr != nil {
		st.LastError = runErr.Error()
	}
	next := end.Add(cfg.QuietPeriod)
	if st.RuntimesToday >= cfg.MaxRuntimesPerDay {
		next = nextDay(end).Add(cfg.QuietPeriod)
	}
	st.LastCheckAt, st.LastCheck, st.NextEligibleAt = end, out, next
	if err := SaveAutoState(saveCtx, s.DB, st); err != nil {
		s.logf("auto re-validation: saving state: %v", err)
	}
	s.logf("auto re-validation: %s: %s: %d ok, %d failed, %d cancelled", plan.Runtime, out, sum.OK, sum.Failed, sum.Cancelled)
	return out
}

// watch cancels a run in progress when the app gets busy or the setting is
// turned off. stop ends the watch and waits for it; it may be called twice.
func (s *AutoScheduler) watch(ctx context.Context, cancel context.CancelCauseFunc) (stop func()) {
	every := s.WatchInterval
	if every <= 0 {
		every = autoDefaultWatch
	}
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-quit:
				return
			case <-ctx.Done():
				return
			case <-t.C:
			}
			if cfg, err := LoadAutoConfig(ctx, s.DB); err == nil && !cfg.Enabled {
				cancel(errAutoTurnedOff)
				return
			}
			if busy, why := s.Busy(ctx); busy {
				cancel(fmt.Errorf("%w (%s)", errAutoBusy, why))
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(quit) })
		<-done
	}
}

func withReason(check, why string) string {
	if why == "" {
		return check
	}
	return check + ": " + why
}

func (s *AutoScheduler) markBusy(t time.Time) {
	s.mu.Lock()
	s.lastBusy = t
	s.mu.Unlock()
}

// record stores the latest decision when it changed; a repeated decision
// is not rewritten every minute. next is when a run may start at the
// earliest; zero when that depends on something else ending (busy, locked).
// A state that can't be read is left alone.
func (s *AutoScheduler) record(ctx context.Context, now time.Time, check string, next time.Time) string {
	st, err := LoadAutoState(ctx, s.DB, now)
	if err != nil {
		s.logf("auto re-validation: reading state: %v", err)
		return check
	}
	if st.LastCheck == check && st.NextEligibleAt.Equal(next) {
		return check
	}
	st.LastCheckAt, st.LastCheck, st.NextEligibleAt = now, check, next
	if err := SaveAutoState(ctx, s.DB, st); err != nil {
		s.logf("auto re-validation: saving state: %v", err)
	}
	return check
}

func (s *AutoScheduler) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// nextDay is local midnight after t: the daily cap follows the daemon's
// local time zone.
func nextDay(t time.Time) time.Time {
	l := t.Local()
	return time.Date(l.Year(), l.Month(), l.Day()+1, 0, 0, 0, 0, l.Location())
}
