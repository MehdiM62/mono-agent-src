package agentroster

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Automatic re-validation (monoes/mono-agent#230): the daemon re-checks
// stale roster entries in the background, one runtime at a time, only while
// nothing else runs, at most a few runtimes a day. Every check is a real,
// paid model call, so it is off until the user turns it on.

// Auto re-validation defaults and limits.
const (
	DefaultAutoRuntimesPerDay = 1
	DefaultAutoModelsPerRun   = 3
	DefaultAutoQuietPeriod    = 15 * time.Minute
	MaxAutoRuntimesPerDay     = 24
	MaxAutoModelsPerRun       = 20
	MinAutoQuietPeriod        = time.Minute
)

// Settings keys (global `settings` table: the roster is machine-wide).
const (
	autoConfigKey = "agent_roster.auto_revalidate"
	autoStateKey  = "agent_roster.auto_revalidate.state"
)

// AutoConfig is the user's auto re-validation setting.
type AutoConfig struct {
	Enabled           bool          `json:"enabled"`
	MaxRuntimesPerDay int           `json:"max_runtimes_per_day"`
	MaxModelsPerRun   int           `json:"max_models_per_run"`
	QuietPeriod       time.Duration `json:"quiet_period_ns"`
}

// DefaultAutoConfig is the setting before the user changes it: off.
func DefaultAutoConfig() AutoConfig {
	return AutoConfig{
		MaxRuntimesPerDay: DefaultAutoRuntimesPerDay,
		MaxModelsPerRun:   DefaultAutoModelsPerRun,
		QuietPeriod:       DefaultAutoQuietPeriod,
	}
}

// Normalize fills zero values with the defaults and clamps to the limits.
func (c AutoConfig) Normalize() AutoConfig {
	d := DefaultAutoConfig()
	if c.MaxRuntimesPerDay <= 0 {
		c.MaxRuntimesPerDay = d.MaxRuntimesPerDay
	}
	c.MaxRuntimesPerDay = min(c.MaxRuntimesPerDay, MaxAutoRuntimesPerDay)
	if c.MaxModelsPerRun <= 0 {
		c.MaxModelsPerRun = d.MaxModelsPerRun
	}
	c.MaxModelsPerRun = min(c.MaxModelsPerRun, MaxAutoModelsPerRun)
	if c.QuietPeriod <= 0 {
		c.QuietPeriod = d.QuietPeriod
	}
	c.QuietPeriod = max(c.QuietPeriod, MinAutoQuietPeriod)
	return c
}

// AutoState is what the scheduler did, persisted so the daily cap survives
// a daemon restart and `auto-revalidate status` can show it.
type AutoState struct {
	Day             string    `json:"day"` // local date (daemon's time zone) the counts below belong to
	RuntimesToday   int       `json:"runtimes_today"`
	SpentTodayUSD   float64   `json:"spent_today_usd"`
	UnknownCostCall int       `json:"unknown_cost_calls_today"` // calls that reported no cost
	LastRunAt       time.Time `json:"last_run_at,omitempty"`
	LastRuntime     string    `json:"last_runtime,omitempty"`
	LastSummary     *Summary  `json:"last_summary,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
	// LastCheckAt/LastCheck: the scheduler's latest decision ("off",
	// "busy: …", "quiet", "daily cap reached", "nothing stale", "ran").
	LastCheckAt    time.Time `json:"last_check_at,omitempty"`
	LastCheck      string    `json:"last_check,omitempty"`
	NextEligibleAt time.Time `json:"next_eligible_at,omitempty"`
}

// rollDay resets the daily counts when now is on a later local day.
func (s *AutoState) rollDay(now time.Time) {
	day := now.Local().Format("2006-01-02")
	if s.Day != day {
		s.Day, s.RuntimesToday, s.SpentTodayUSD, s.UnknownCostCall = day, 0, 0, 0
	}
}

// LoadAutoConfig reads the setting; missing means DefaultAutoConfig (off).
func LoadAutoConfig(ctx context.Context, db *sql.DB) (AutoConfig, error) {
	c := DefaultAutoConfig()
	raw, ok, err := getSetting(ctx, db, autoConfigKey)
	if err != nil || !ok {
		return c, err
	}
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return DefaultAutoConfig(), fmt.Errorf("reading %s: %w", autoConfigKey, err)
	}
	return c.Normalize(), nil
}

// SaveAutoConfig stores the setting.
func SaveAutoConfig(ctx context.Context, db *sql.DB, c AutoConfig) error {
	b, _ := json.Marshal(c.Normalize())
	return setSetting(ctx, db, autoConfigKey, string(b))
}

// ErrAutoStateCorrupt is LoadAutoState's error for a stored state that
// isn't valid JSON, as opposed to one that couldn't be read right now.
var ErrAutoStateCorrupt = errors.New("auto re-validation state is corrupt")

// LoadAutoState reads the scheduler's state, with today's counts rolled
// over when the stored day is past.
func LoadAutoState(ctx context.Context, db *sql.DB, now time.Time) (AutoState, error) {
	var s AutoState
	raw, ok, err := getSetting(ctx, db, autoStateKey)
	if err == nil && ok {
		if jerr := json.Unmarshal([]byte(raw), &s); jerr != nil {
			err = fmt.Errorf("%w: %w", ErrAutoStateCorrupt, jerr)
		}
	}
	s.rollDay(now)
	return s, err
}

// ResetCorruptAutoState replaces a corrupt stored state with an empty one
// and reports whether it did. Any other read error (a busy database) is
// returned and nothing is written: resetting then would wipe today's count
// and allow more paid runs than the cap.
func ResetCorruptAutoState(ctx context.Context, db *sql.DB, now time.Time) (bool, error) {
	_, err := LoadAutoState(ctx, db, now)
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, ErrAutoStateCorrupt):
		return true, SaveAutoState(ctx, db, AutoState{})
	default:
		return false, err
	}
}

// SaveAutoState stores the scheduler's state.
func SaveAutoState(ctx context.Context, db *sql.DB, s AutoState) error {
	b, _ := json.Marshal(s)
	return setSetting(ctx, db, autoStateKey, string(b))
}

func getSetting(ctx context.Context, db *sql.DB, key string) (string, bool, error) {
	var v string
	err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func setSetting(ctx context.Context, db *sql.DB, key, value string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// AutoPlan is one automatic run: the stale models of one runtime and the
// estimated cost from their earlier results.
type AutoPlan struct {
	Runtime     string   `json:"runtime"`
	Targets     []Target `json:"targets"`
	EstCostUSD  float64  `json:"est_cost_usd"`
	UnknownCost int      `json:"unknown_cost"`
	// TableEstimated counts targets priced from the built-in table.
	TableEstimated int `json:"table_estimated"`
}

// PickStale chooses the next automatic run from a built roster: the
// installed runtime whose oldest stale model is oldest, and up to maxModels
// of its stale models, oldest first. Only stale entries (passed once, now
// too old or on another runtime version) are picked: failed and untested
// ones need the user. version is the runtime's installed version, recorded
// on the targets. Nil when nothing is stale.
func PickStale(roster []RuntimeRoster, previous []Result, maxModels int) *AutoPlan {
	if maxModels <= 0 {
		maxModels = DefaultAutoModelsPerRun
	}
	var best *RuntimeRoster
	var bestStale []Entry
	for i := range roster {
		rr := &roster[i]
		if !rr.Installed {
			continue
		}
		var stale []Entry
		for _, e := range rr.Models {
			if e.State == StateStale {
				stale = append(stale, e)
			}
		}
		if len(stale) == 0 {
			continue
		}
		sort.SliceStable(stale, func(a, b int) bool { return stale[a].ValidatedAt.Before(stale[b].ValidatedAt) })
		if best == nil || stale[0].ValidatedAt.Before(bestStale[0].ValidatedAt) {
			best, bestStale = rr, stale
		}
	}
	if best == nil {
		return nil
	}
	if len(bestStale) > maxModels {
		bestStale = bestStale[:maxModels]
	}
	p := &AutoPlan{Runtime: best.Runtime}
	for _, e := range bestStale {
		p.Targets = append(p.Targets, Target{
			Runtime: best.Runtime, Model: e.Model, Label: e.Label, EffortLevels: e.EffortLevels,
			Source: orDefault(e.Source, SourceListed), RuntimeVersion: best.Version,
		})
	}
	plan := Plan{Targets: p.Targets}
	plan.EstimateCost(previous)
	p.EstCostUSD, p.UnknownCost, p.TableEstimated = plan.EstCostUSD, plan.UnknownCost, plan.TableEstimated
	return p
}

// DailyCeiling is the most a day of automatic runs can cost: every run
// at the cap, every model priced like the priciest model with a known cost
// in previous. known is false when no model has reported a cost yet.
func DailyCeiling(c AutoConfig, previous []Result) (perDay, priciest float64, known bool) {
	c = c.Normalize()
	for _, r := range previous {
		if r.HasCost && r.CostUSD > priciest {
			priciest, known = r.CostUSD, true
		}
	}
	return float64(c.MaxRuntimesPerDay*c.MaxModelsPerRun) * priciest, priciest, known
}

// AutoRunResult is what one automatic run did.
type AutoRunResult struct {
	Summary     Summary
	SpentUSD    float64 // reported (or estimated) cost of the calls made
	UnknownCost int     // calls that reported no cost
}

// Add counts one progress line's cost. A call cancelled mid-flight (the
// app got busy, the setting was turned off, shutdown) was still made and
// may still be billed: its reported cost counts, or it counts as unknown.
func (r *AutoRunResult) Add(l Line) {
	if l.Type != "validate.result" || l.Result == nil {
		return
	}
	if l.Result.HasCost {
		r.SpentUSD += l.Result.CostUSD
	} else {
		r.UnknownCost++
	}
}
