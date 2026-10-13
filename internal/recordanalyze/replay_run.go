package recordanalyze

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/action"
	"github.com/monoes/mono-agent/internal/browser"
)

// StatusSelfHealed marks a step whose recorded selectors all missed and
// that the opt-in self-heal (Jev) found the element for.
const StatusSelfHealed = "self_healed"

// ReplayRunOptions controls RunReplay.
type ReplayRunOptions struct {
	Page   browser.PageInterface
	Logger zerolog.Logger
	// Secrets resolves {{secret:x}} values no input supplied (the vault).
	Secrets func(name string) (string, bool)
	// SelfHeal, when set, is applied to every executor: production installs
	// the Jev element picker, which the executor consults only for a step
	// whose recorded selectors all missed. Nil (the default) never heals.
	SelfHeal func(*action.ActionExecutor)
	// OnSelfHeal is told about each step self-heal rescued.
	OnSelfHeal func(step ReplayStep)
	// Confirm is asked before each step that changes the site; it runs only
	// on true. Nil stops before the first such step.
	Confirm func(step ReplayStep) bool
}

// ReplayReport is `record replay --json`.
type ReplayReport struct {
	RecordingID string       `json:"recordingId"`
	Steps       []StepReport `json:"steps"`
	OK          bool         `json:"ok"`
	// StoppedAt is the side-effect step the replay stopped before (not
	// confirmed, or no confirmation possible): an incomplete but clean run.
	StoppedAt  string   `json:"stoppedAt,omitempty"`
	SelfHealed []string `json:"selfHealed,omitempty"`
	Skipped    []string `json:"skipped,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// healObserver records selector lookups and reports self-heals as they
// happen: a lookup that succeeded only after every recorded candidate
// missed (index -1) was rescued by the fallback.
type healObserver struct {
	observer
	healed map[string]bool // config key → self-healed
	onHeal func(key string)
}

func (o *healObserver) ObserveSelector(id, key string, idx int, ok, healed bool) {
	o.observer.ObserveSelector(id, key, idx, ok, healed)
	if ok && healed && idx < 0 {
		o.healed[key] = true
		if o.onHeal != nil {
			o.onHeal(key)
		}
	}
}

// RunReplay runs plan on the page. Steps run in order with the recorded
// selectors only; the run is split before every side-effect step, which
// runs only when Confirm says yes. The page is left as the run left it.
func RunReplay(ctx context.Context, plan *ReplayPlan, o ReplayRunOptions) *ReplayReport {
	rep := &ReplayReport{RecordingID: plan.RecordingID, Steps: []StepReport{}, Skipped: plan.Skipped, OK: true}
	byID := map[string]ReplayStep{}
	for _, s := range plan.Steps {
		byID[s.ID] = s
	}
	chunks := splitAtSideEffects(plan.Def.Steps)
	for ci, chunk := range chunks {
		if chunk[0].SideEffect && (o.Confirm == nil || !o.Confirm(byID[chunk[0].ID])) {
			rep.StoppedAt = chunk[0].ID
			for i, s := range remaining(chunks[ci:]) {
				r := StepReport{ID: s.ID, Type: s.Type, Status: StatusSkipped}
				if i == 0 {
					r.Status, r.Message = StatusStopped, "not run: "+byID[s.ID].Describe+" changes the site"
				}
				rep.Steps = append(rep.Steps, r)
			}
			return rep
		}
		rows, errMsg := runChunk(ctx, plan, chunk, o, byID, rep)
		rep.Steps = append(rep.Steps, rows...)
		failed := errMsg != ""
		for _, r := range rows {
			failed = failed || r.Status == StatusFail
		}
		if failed {
			rep.OK = false
			rep.Error = errMsg
			for _, s := range remaining(chunks[ci+1:]) {
				rep.Steps = append(rep.Steps, StepReport{ID: s.ID, Type: s.Type, Status: StatusSkipped})
			}
			return rep
		}
	}
	return rep
}

// runChunk runs one run of steps; the message is the (redacted) error that
// ended it, "" when it completed.
func runChunk(ctx context.Context, plan *ReplayPlan, steps []action.StepDef, o ReplayRunOptions, byID map[string]ReplayStep, rep *ReplayReport) ([]StepReport, string) {
	def := *plan.Def
	def.Steps = steps
	keyStep := map[string]string{}
	for _, s := range steps {
		if s.ConfigKey != "" {
			keyStep[s.ConfigKey] = s.ID
		}
	}
	obs := &healObserver{healed: map[string]bool{}, onHeal: func(key string) {
		st := byID[keyStep[key]]
		o.Logger.Warn().Str("step", st.ID).Str("intent", st.Describe).
			Msg("self-heal: every recorded selector missed; the fallback picked the element")
		if o.OnSelfHeal != nil {
			o.OnSelfHeal(st)
		}
	}}
	events := make(chan action.ExecutionEvent, 8192)
	ae := action.NewActionExecutor(ctx, o.Page, nil, nil, events, nil, o.Logger)
	ae.SetPackage(plan.Pkg)
	// A chunk holds at most one side-effect step, its first, and only
	// once it was confirmed; safe mode guards everything else.
	ae.SetSafeMode(!steps[0].SideEffect)
	ae.SetSelectorObserver(obs)
	if o.Secrets != nil {
		ae.SetSecretLookup(func(_, name string) (string, bool) { return o.Secrets(name) })
	}
	params := map[string]interface{}{}
	for k, v := range plan.Inputs {
		params[k] = v
		ae.SetVariable(k, v)
	}
	if o.SelfHeal != nil {
		o.SelfHeal(ae)
	}
	res, err := ae.ExecuteDef(&action.StorageAction{
		ID: "replay-" + time.Now().UTC().Format("20060102T150405"), Type: def.ActionType,
		TargetPlatform: ReplayAutomationID, Params: params,
	}, &def)
	close(events)
	out := RunOutcome{Result: res, Err: err, SafeStop: ae.SafeStopped()}
	for ev := range events {
		out.Events = append(out.Events, ev)
	}
	r := BuildReport(&def, out, obs.obs, plan.Pkg)
	secrets := map[string]any{}
	for k, v := range plan.Inputs {
		secrets[k] = v
	}
	redact(r, secrets)
	for i, row := range r.Steps {
		for key, sid := range keyStep {
			if sid == row.ID && obs.healed[key] && row.Status == StatusHealed {
				r.Steps[i].Status = StatusSelfHealed
				r.Steps[i].Message = "every recorded selector missed; self-heal found the element"
				rep.SelfHealed = append(rep.SelfHealed, row.ID)
			}
		}
	}
	if out.SafeStop != nil && r.Error == "" {
		// Cannot happen: every side-effect step starts a chunk and a
		// chunk runs in safe mode only when its first step has none.
		r.Error = fmt.Sprintf("stopped before %s unexpectedly", out.SafeStop.StepID)
	}
	return r.Steps, r.Error
}

// splitAtSideEffects cuts steps into runs that each start at a side-effect
// step (the first run may have none).
func splitAtSideEffects(steps []action.StepDef) [][]action.StepDef {
	var out [][]action.StepDef
	for _, s := range steps {
		if len(out) == 0 || s.SideEffect {
			out = append(out, nil)
		}
		out[len(out)-1] = append(out[len(out)-1], s)
	}
	return out
}

func remaining(chunks [][]action.StepDef) []action.StepDef {
	var out []action.StepDef
	for _, c := range chunks {
		out = append(out, c...)
	}
	return out
}
