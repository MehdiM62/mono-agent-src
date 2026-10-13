package recordanalyze

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/monoes/mono-agent/internal/action"
)

// Deterministic replay of a recording (`record replay`): the recorded
// steps, in order, with each element's recorded selector candidates as its
// fallbacks — no model involved. Normalize and Detect (both deterministic)
// clean the steps and mark the ones that change the site; PlanReplay turns
// them into an action definition the ordinary ActionExecutor runs against
// an in-memory package (replayPkg) holding the recorded selectors.

// ReplayAutomationID is the package id a replay runs under.
const ReplayAutomationID = "recording-replay"

// ReplayStep is one planned step, for listing and confirmation prompts.
type ReplayStep struct {
	ID         string `json:"id"` // the recorded event id
	Type       string `json:"type"`
	Describe   string `json:"describe"`
	SideEffect bool   `json:"sideEffect,omitempty"`
	Input      string `json:"input,omitempty"`
	Selectors  int    `json:"selectors,omitempty"` // recorded candidates, tried in order
}

// ReplayPlan is a recording turned into something runnable.
type ReplayPlan struct {
	RecordingID string                `json:"recordingId"`
	StartURL    string                `json:"startUrl"`
	Steps       []ReplayStep          `json:"steps"`
	Skipped     []string              `json:"skipped,omitempty"` // "e4: extract marks are not replayed"
	Inputs      map[string]any        `json:"-"`                 // recorded values plus overrides
	InputNames  []string              `json:"inputs"`
	Missing     []string              `json:"missingInputs,omitempty"` // secrets nobody supplied
	Def         *action.ActionDef     `json:"-"`
	Pkg         action.PackageContext `json:"-"`
}

// ReplayOptions controls PlanReplay.
type ReplayOptions struct {
	// Inputs override recorded values, by input name (see the plan's
	// InputNames); masked secrets have no recorded value and need one.
	Inputs map[string]any
	// WithLogin keeps a detected login flow; by default the replay starts
	// where the login led, like an analyzed action.
	WithLogin bool
	// HasSecret reports a vault secret for a masked input's name.
	HasSecret func(name string) bool
}

// PlanReplay turns a recording into a replay plan.
func PlanReplay(rec *Recording, opts ReplayOptions) (*ReplayPlan, error) {
	if rec == nil || len(rec.Events) == 0 {
		return nil, fmt.Errorf("the recording has no events")
	}
	norm := Normalize(rec.Summary, rec.Events)
	a := Detect(norm)
	plan := &ReplayPlan{RecordingID: norm.RecordingID, StartURL: a.ActionFrom, Inputs: map[string]any{}}
	if opts.WithLogin || plan.StartURL == "" {
		plan.StartURL = norm.StartURL
	}
	specs := map[string]InputSpec{}
	for _, in := range a.Inputs {
		specs[in.Name] = in
	}
	pkg := &replayPkg{start: plan.StartURL, domains: norm.Domains, selectors: map[string]*action.SelectorEntry{}}
	def := &action.ActionDef{ActionType: "replay", Automation: ReplayAutomationID, Platform: ReplayAutomationID,
		Description: "Replay of recording " + plan.RecordingID}
	used := map[string]bool{}
	var steps []action.StepDef
	first := true
	for i := range norm.Steps {
		s := &norm.Steps[i]
		if s.Login && !opts.WithLogin {
			plan.Skipped = append(plan.Skipped, s.EventID+": part of the login (use --with-login to replay it)")
			continue
		}
		if first && s.Kind != KindNavigate && plan.StartURL != "" {
			steps = append(steps, action.StepDef{ID: "start", Type: "navigate", URL: plan.StartURL})
			plan.Steps = append(plan.Steps, ReplayStep{ID: "start", Type: "navigate", Describe: "open " + plan.StartURL})
		}
		first = false
		st, why := replayStepDef(s, pkg)
		if why != "" {
			if s.Kind == KindUpload {
				return nil, fmt.Errorf("step %s: %s", s.EventID, why)
			}
			plan.Skipped = append(plan.Skipped, s.EventID+": "+why)
			continue
		}
		if s.Input != "" && (st.Type == "type" || st.Type == "select_option") {
			used[s.Input] = true
		}
		steps = append(steps, st)
		plan.Steps = append(plan.Steps, ReplayStep{ID: st.ID, Type: st.Type, Describe: describeReplay(s),
			SideEffect: st.SideEffect, Input: s.Input, Selectors: len(s.Candidates)})
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("the recording has no steps to replay")
	}
	def.Steps = steps

	var required []json.RawMessage
	for name := range used {
		plan.InputNames = append(plan.InputNames, name)
	}
	sort.Strings(plan.InputNames)
	for _, name := range plan.InputNames {
		spec := specs[name]
		typ := "string"
		if spec.Type == "secret" {
			typ = "secret"
		}
		raw, _ := json.Marshal(map[string]any{"name": name, "type": typ})
		required = append(required, raw)
		if v, ok := opts.Inputs[name]; ok {
			plan.Inputs[name] = v
		} else if typ != "secret" {
			plan.Inputs[name] = spec.Default
		} else if opts.HasSecret == nil || !opts.HasSecret(name) {
			plan.Missing = append(plan.Missing, name)
		}
	}
	def.Inputs = &action.InputDef{Required: required}
	plan.Def, plan.Pkg = def, pkg
	return plan, nil
}

// replayStepDef maps one normalized step to an executor step, or explains
// why it is not replayed.
func replayStepDef(s *Step, pkg *replayPkg) (action.StepDef, string) {
	st := action.StepDef{ID: s.EventID, SideEffect: s.SideEffect, Until: s.Until}
	element := func() string {
		if len(s.Candidates) == 0 {
			return "no recorded selector for its element"
		}
		key := "el_" + s.EventID
		pkg.selectors[key] = &action.SelectorEntry{Candidates: s.Candidates, Intent: intentOf(s)}
		st.ConfigKey, st.Intent = key, intentOf(s)
		return ""
	}
	value := func() {
		if s.Masked {
			st.Value = "{{secret:" + s.Input + "}}"
		} else {
			st.Value = "{{" + s.Input + "}}"
		}
	}
	switch s.Kind {
	case KindNavigate:
		st.Type, st.URL = "navigate", s.URL
		return st, ""
	case KindClick, KindCheck:
		st.Type = "click"
		return st, element()
	case KindType:
		st.Type = "type"
		value()
		return st, element()
	case KindSelect:
		st.Type = "select_option"
		value()
		return st, element()
	case KindPressKey:
		st.Type, st.Key = "press_key", s.Key
		if s.Target != nil && len(s.Candidates) > 0 {
			return st, element()
		}
		return st, ""
	case KindSubmit:
		// A form submitted with no click or key recorded before it:
		// Enter in the field that has focus submits it again.
		st.Type, st.Key = "press_key", "Enter"
		return st, ""
	case KindUpload:
		return st, "replay does not upload files"
	case KindExtract:
		return st, "extract marks are not replayed"
	case KindScroll:
		return st, "scrolls are not replayed"
	}
	return st, "unsupported step " + s.Kind
}

// intentOf names a step's element in plain words, for the self-heal
// fallback (Jev) and the confirmation prompt.
func intentOf(s *Step) string {
	fp := s.Target
	if fp == nil {
		return ""
	}
	label := firstNonEmpty(fp.AriaName, fp.Label, fp.Text, fp.Placeholder, fp.Name)
	what := firstNonEmpty(fp.Role, fp.Tag, "element")
	if fp.Tag == "input" || fp.Tag == "textarea" {
		what = "field"
	}
	if label == "" {
		return "the " + what
	}
	return fmt.Sprintf("the %q %s", clip(label, 60), what)
}

func describeReplay(s *Step) string {
	switch s.Kind {
	case KindNavigate:
		return "open " + s.URL
	case KindType, KindSelect:
		v := s.Value
		if s.Masked {
			v = "•••"
		}
		return fmt.Sprintf("%s %q into %s", map[string]string{KindType: "type", KindSelect: "choose"}[s.Kind], clip(v, 40), intentOf(s))
	case KindPressKey, KindSubmit:
		k := firstNonEmpty(s.Key, "Enter")
		if t := intentOf(s); t != "" && s.Kind == KindPressKey {
			return "press " + k + " in " + t
		}
		return "press " + k
	}
	return s.Kind + " " + intentOf(s)
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}

// replayPkg is the in-memory package a replay runs under: the recorded
// selectors, the recorded domains as the allowlist, recorded trust (so the
// rules a recorded package gets apply), and no scripts.
type replayPkg struct {
	start     string
	domains   []string
	selectors map[string]*action.SelectorEntry
}

func (p *replayPkg) ID() string                    { return ReplayAutomationID }
func (p *replayPkg) StartURL() string              { return p.start }
func (p *replayPkg) Domains() []string             { return p.domains }
func (p *replayPkg) PermittedSteps() []string      { return nil }
func (p *replayPkg) Trust() string                 { return "recorded" }
func (p *replayPkg) ScriptsAllowed() bool          { return false }
func (p *replayPkg) Script(string) (string, error) { return "", fmt.Errorf("a replay has no scripts") }
func (p *replayPkg) Fragment(name string) (*action.FragmentDef, error) {
	return nil, fmt.Errorf("a replay has no fragment %q", name)
}
func (p *replayPkg) ResolveAction(ref string) (*action.ActionDef, action.PackageContext, error) {
	return nil, nil, fmt.Errorf("a replay cannot call action %q", ref)
}
func (p *replayPkg) Selector(key string) (*action.SelectorEntry, bool) {
	e, ok := p.selectors[key]
	return e, ok
}
