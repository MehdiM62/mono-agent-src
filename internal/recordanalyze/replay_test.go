package recordanalyze

import (
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/action"
)

func formPlan(t *testing.T, inputs map[string]any) *ReplayPlan {
	t.Helper()
	plan, err := PlanReplay(loadFixture(t, "form-submit"), ReplayOptions{Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func sideEffectStep(t *testing.T, plan *ReplayPlan) ReplayStep {
	t.Helper()
	for _, s := range plan.Steps {
		if s.SideEffect {
			return s
		}
	}
	t.Fatalf("no side-effect step in %+v", plan.Steps)
	return ReplayStep{}
}

func TestPlanReplayFormSubmit(t *testing.T) {
	plan := formPlan(t, nil)
	var types []string
	for _, s := range plan.Steps {
		types = append(types, s.Type)
	}
	if got := strings.Join(types, ","); got != "navigate,type,type,type,click" {
		t.Fatalf("steps = %s (%+v)", got, plan.Steps)
	}
	save := sideEffectStep(t, plan)
	if save.Type != "click" || !strings.Contains(save.Describe, "Save") || save.Selectors != 2 {
		t.Errorf("save step = %+v", save)
	}
	if strings.Join(plan.Missing, ",") != "account_password" {
		t.Errorf("missing = %v", plan.Missing)
	}
	// Typed values go through variables, the masked one through a secret template.
	var values []string
	for _, s := range plan.Def.Steps {
		if v, ok := s.Value.(string); ok {
			values = append(values, v)
		}
	}
	if !strings.Contains(strings.Join(values, " "), "{{secret:account_password}}") {
		t.Errorf("values = %v", values)
	}
	if plan.Inputs["full_name"] != "Jane Doe" {
		t.Errorf("recorded values = %v", plan.Inputs)
	}
	// Every element step resolves through the recorded candidates.
	for _, s := range plan.Def.Steps {
		if s.Type == "navigate" {
			continue
		}
		e, ok := plan.Pkg.Selector(s.ConfigKey)
		if !ok || len(e.Candidates) == 0 || s.Intent == "" {
			t.Errorf("step %s has no recorded selectors", s.ID)
		}
	}

	plan = formPlan(t, map[string]any{"account_password": "pw", "full_name": "Ann"})
	if len(plan.Missing) != 0 || plan.Inputs["full_name"] != "Ann" {
		t.Errorf("overrides not applied: missing=%v inputs=%v", plan.Missing, plan.Inputs)
	}
}

func TestRunReplayStopsBeforeSideEffectWithoutConfirmation(t *testing.T) {
	plan := formPlan(t, map[string]any{"account_password": "s3cret"})
	page := newFormPage()
	healCalls := 0
	rep := RunReplay(context.Background(), plan, ReplayRunOptions{Page: page, Logger: zerolog.Nop(),
		SelfHeal: func(*action.ActionExecutor) { healCalls++ }})
	if page.elems[`[data-testid="contact-save"]`].clicked {
		t.Fatal("replay clicked Save without confirmation")
	}
	if rep.StoppedAt != sideEffectStep(t, plan).ID || !rep.OK {
		t.Errorf("report = %+v", rep)
	}
	last := rep.Steps[len(rep.Steps)-1]
	if last.Status != StatusStopped {
		t.Errorf("last row = %+v", last)
	}
	for _, s := range rep.Steps[:len(rep.Steps)-1] {
		if s.Status != StatusPass {
			t.Errorf("step %s = %s (%s)", s.ID, s.Status, s.Message)
		}
	}
	typed := strings.Join(page.typed, "|")
	if !strings.Contains(typed, "jane@example.com") || !strings.Contains(typed, "s3cret") {
		t.Errorf("typed %d values, missing expected ones", len(page.typed))
	}
	// The hook only installs the fallback; nothing here needed it.
	if healCalls != 1 || len(rep.SelfHealed) != 0 {
		t.Errorf("heal hook calls=%d selfHealed=%v", healCalls, rep.SelfHealed)
	}
}

func TestRunReplayRunsConfirmedSideEffect(t *testing.T) {
	plan := formPlan(t, map[string]any{"account_password": "pw"})
	page := newFormPage()
	var asked []string
	rep := RunReplay(context.Background(), plan, ReplayRunOptions{Page: page, Logger: zerolog.Nop(),
		Confirm: func(s ReplayStep) bool { asked = append(asked, s.ID); return true }})
	if !page.elems[`[data-testid="contact-save"]`].clicked || !rep.OK || rep.StoppedAt != "" {
		t.Fatalf("confirmed Save did not run: %+v", rep)
	}
	if len(asked) != 1 || asked[0] != sideEffectStep(t, plan).ID {
		t.Errorf("asked = %v", asked)
	}
	if page.url != "https://app.acme-crm.test/contacts/4821" {
		t.Errorf("url = %s", page.url)
	}
}

func TestRunReplayDeclinedConfirmationStops(t *testing.T) {
	plan := formPlan(t, map[string]any{"account_password": "pw"})
	page := newFormPage()
	rep := RunReplay(context.Background(), plan, ReplayRunOptions{Page: page, Logger: zerolog.Nop(),
		Confirm: func(ReplayStep) bool { return false }})
	if page.elems[`[data-testid="contact-save"]`].clicked || rep.StoppedAt == "" {
		t.Fatalf("declined Save ran: %+v", rep)
	}
}

func TestRunReplayUsesRecordedFallbackSelector(t *testing.T) {
	plan := formPlan(t, map[string]any{"account_password": "pw"})
	page := newFormPage()
	// The page changed: the email field lost its test id but kept #email,
	// the recording's third candidate.
	page.elems["#email"] = page.elems[`[data-testid="contact-email"]`]
	delete(page.elems, `[data-testid="contact-email"]`)
	rep := RunReplay(context.Background(), plan, ReplayRunOptions{Page: page, Logger: zerolog.Nop()})
	if !rep.OK {
		t.Fatalf("report = %+v", rep)
	}
	if rep.Steps[1].Status != StatusHealed || len(rep.SelfHealed) != 0 {
		t.Errorf("email row = %+v selfHealed=%v", rep.Steps[1], rep.SelfHealed)
	}
}

func TestRunReplayFailsWhenSelectorsMissAndNoSelfHeal(t *testing.T) {
	plan := formPlan(t, map[string]any{"account_password": "zq9-pw"})
	page := newFormPage()
	delete(page.elems, `input[name="full_name"]`)
	rep := RunReplay(context.Background(), plan, ReplayRunOptions{Page: page, Logger: zerolog.Nop()})
	if rep.OK || rep.Error == "" {
		t.Fatalf("report = %+v", rep)
	}
	statuses := map[string]int{}
	for _, s := range rep.Steps {
		statuses[s.Status]++
	}
	if statuses[StatusFail] != 1 || statuses[StatusSkipped] == 0 || page.elems[`[data-testid="contact-save"]`].clicked {
		t.Errorf("rows = %+v", rep.Steps)
	}
	if strings.Contains(rep.Error, "zq9-pw") {
		t.Errorf("error echoes an input: %s", rep.Error)
	}
}

func TestHealObserverReportsOnlyFallbackPicks(t *testing.T) {
	var keys []string
	o := &healObserver{healed: map[string]bool{}, onHeal: func(k string) { keys = append(keys, k) }}
	o.ObserveSelector("a", "k1", 0, true, false)   // first candidate
	o.ObserveSelector("a", "k2", 2, true, true)    // a recorded fallback
	o.ObserveSelector("a", "k3", -1, false, false) // nothing found
	o.ObserveSelector("a", "k4", -1, true, true)   // every candidate missed, the fallback found it
	if strings.Join(keys, ",") != "k4" || len(o.obs) != 4 {
		t.Errorf("keys = %v obs = %d", keys, len(o.obs))
	}
}

func TestSplitAtSideEffects(t *testing.T) {
	steps := []action.StepDef{{ID: "a"}, {ID: "b", SideEffect: true}, {ID: "c"}, {ID: "d", SideEffect: true}}
	chunks := splitAtSideEffects(steps)
	if len(chunks) != 3 || chunks[1][0].ID != "b" || len(chunks[1]) != 2 || chunks[2][0].ID != "d" {
		t.Errorf("chunks = %+v", chunks)
	}
	if got := splitAtSideEffects([]action.StepDef{{ID: "x", SideEffect: true}}); len(got) != 1 || len(got[0]) != 1 {
		t.Errorf("leading side effect: %+v", got)
	}
}
