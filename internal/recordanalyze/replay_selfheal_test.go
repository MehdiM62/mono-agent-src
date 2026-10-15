package recordanalyze

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/action"
	"github.com/monoes/mono-agent/internal/browser"
	"github.com/monoes/mono-agent/internal/jev"
	"github.com/monoes/mono-agent/internal/jev/jevtest"
	"github.com/monoes/mono-agent/internal/jevpick"
)

// jevFormPage is the form page with a CDP endpoint that answers jevpick's
// snapshot, freshness, mark, unmark and fingerprint evaluations, so the
// real Jev element picker (against a jevtest server) can run on it.
type jevFormPage struct {
	*fakePage
	state   jevpick.PageState
	nodes   map[int]*fakeElem // snapshot node → element
	marked  map[string]int    // marker → node
	fpCalls int
}

var (
	jevMarkRe    = regexp.MustCompile(`nodes\.get\((\d+)\); if \(!e\?\.isConnected\) return false; e\.setAttribute\("data-monoagent-jev","([0-9a-f]+)"\)`)
	jevUnmarkRe  = regexp.MustCompile(`querySelectorAll\('\[data-monoagent-jev="([0-9a-f]+)"\]'\)`)
	jevGuardRe   = regexp.MustCompile(`nodes\.get\((\d+)\)`)
	jevMarkSelRe = regexp.MustCompile(`^\[data-monoagent-jev='([0-9a-f]{16})'\]$`)
)

func newJevFormPage() *jevFormPage {
	page := newFormPage()
	return &jevFormPage{
		fakePage: page,
		state: jevpick.PageState{
			URL: "https://app.acme-crm.test/contacts/new", Title: "New contact", Text: "New contact",
			Marker: json.RawMessage(`["m"]`), PageKey: json.RawMessage(`["k"]`),
			Guards: map[string]json.RawMessage{"10": json.RawMessage(`["g10"]`), "20": json.RawMessage(`["g20"]`)},
			Actions: []jevpick.Action{
				{ID: "e1", Kind: "fill", Label: "Full name", Role: "textbox", Node: 10},
				{ID: "e2", Kind: "fill", Label: "Email", Role: "textbox", Node: 20},
			},
		},
		nodes:  map[int]*fakeElem{10: page.elems[`input[name="full_name"]`], 20: page.elems[`[data-testid="contact-email"]`]},
		marked: map[string]int{},
	}
}

var fullNameFingerprint = map[string]any{
	"tag": "input", "role": "textbox", "label": "Full name",
	"candidates": []any{
		map[string]any{"kind": "css", "value": "#contact-full-name", "unique": true, "count": 1, "score": 0.9},
	},
}

func (p *jevFormPage) CDP(method string, params map[string]interface{}) (map[string]interface{}, error) {
	if method != "Runtime.evaluate" {
		return map[string]interface{}{}, nil
	}
	expr := params["expression"].(string)
	var value interface{}
	switch {
	case strings.Contains(expr, "SECRET_WORDS"): // jev_fingerprint.js
		p.fpCalls++
		value = fullNameFingerprint
	case strings.Contains(expr, "state?.marker"):
		value = []interface{}{"m"}
	case strings.Contains(expr, "c.pageKey(),c.guard"):
		var g interface{}
		_ = json.Unmarshal(p.state.Guards[jevGuardRe.FindStringSubmatch(expr)[1]], &g)
		value = []interface{}{[]interface{}{"k"}, g}
	case jevMarkRe.MatchString(expr):
		m := jevMarkRe.FindStringSubmatch(expr)
		var node int
		_ = json.Unmarshal([]byte(m[1]), &node)
		p.marked[m[2]] = node
		value = true
	case jevUnmarkRe.MatchString(expr):
		delete(p.marked, jevUnmarkRe.FindStringSubmatch(expr)[1])
		value = true
	default: // the snapshot
		raw, _ := json.Marshal(p.state)
		_ = json.Unmarshal(raw, &value)
	}
	return map[string]interface{}{"result": map[string]interface{}{"value": value}}, nil
}

// Element also resolves the marker selector of a Jev pick to the element
// behind the marked snapshot node.
func (p *jevFormPage) Element(sel string, d time.Duration) (browser.ElementHandle, error) {
	if m := jevMarkSelRe.FindStringSubmatch(sel); m != nil {
		if node, ok := p.marked[m[1]]; ok {
			return p.nodes[node], nil
		}
	}
	return p.fakePage.Element(sel, d)
}

// hideFullName makes every recorded candidate of the full-name field miss
// while the element stays on the page (Jev can still find it).
func (p *jevFormPage) hideFullName() *fakeElem {
	e := p.fakePage.elems[`input[name="full_name"]`]
	delete(p.fakePage.elems, `input[name="full_name"]`)
	return e
}

// jevSelfHeal is the hook `record replay --self-heal` installs, with a
// client of the jevtest server.
func jevSelfHeal(t *testing.T) func(*action.ActionExecutor) {
	t.Helper()
	c, err := jev.NewClient("test-key", "")
	if err != nil {
		t.Fatal(err)
	}
	return func(ae *action.ActionExecutor) { ae.SetJevPicker(c, 0.5) }
}

func fullNameStep(t *testing.T, plan *ReplayPlan) ReplayStep {
	t.Helper()
	for _, s := range plan.Def.Steps {
		if e, ok := plan.Pkg.Selector(s.ConfigKey); ok {
			for _, c := range e.Candidates {
				if c.CSS == `input[name="full_name"]` {
					for _, rs := range plan.Steps {
						if rs.ID == s.ID {
							return rs
						}
					}
				}
			}
		}
	}
	t.Fatal("no full-name step in the plan")
	return ReplayStep{}
}

func stepIntent(plan *ReplayPlan, id string) string {
	for _, s := range plan.Def.Steps {
		if s.ID == id {
			return s.Intent
		}
	}
	return ""
}

// With --self-heal and every recorded candidate missing, Jev is asked once,
// for that step only; the run continues and the step reports self_healed
// with the picked element's selector.
func TestRunReplaySelfHealPicksMissingElement(t *testing.T) {
	srv := jevtest.NewServer(t, jevtest.Fixed(map[string]string{"target": "1"})) // 1 = Full name
	plan := formPlan(t, map[string]any{"account_password": "pw", "full_name": "Ann Lee"})
	step := fullNameStep(t, plan)
	page := newJevFormPage()
	name := page.hideFullName()
	var healed []string
	rep := RunReplay(context.Background(), plan, ReplayRunOptions{Page: page, Logger: zerolog.Nop(),
		SelfHeal:   jevSelfHeal(t),
		OnSelfHeal: func(s ReplayStep) { healed = append(healed, s.ID) },
		Confirm:    func(ReplayStep) bool { return true }})

	if !rep.OK || rep.StoppedAt != "" {
		t.Fatalf("report = %+v", rep)
	}
	if srv.Calls() != 1 {
		t.Fatalf("jev calls = %d, want 1", srv.Calls())
	}
	asked, _ := json.Marshal(srv.Requests()[0].Questions["target"].Instructions)
	if want, _ := json.Marshal(stepIntent(plan, step.ID)); !strings.Contains(string(asked), string(want)) {
		t.Errorf("jev was asked %s, want the intent %s", asked, want)
	}
	if name.value != "Ann Lee" {
		t.Errorf("full name field = %q", name.value)
	}
	if len(healed) != 1 || healed[0] != step.ID || strings.Join(rep.SelfHealed, ",") != step.ID {
		t.Errorf("OnSelfHeal = %v, SelfHealed = %v, want %s", healed, rep.SelfHealed, step.ID)
	}
	for _, r := range rep.Steps {
		switch {
		case r.ID == step.ID:
			if r.Status != StatusSelfHealed || r.Selector != "#contact-full-name" || !strings.Contains(r.Message, "#contact-full-name") {
				t.Errorf("self-healed row = %+v", r)
			}
		case r.Status != StatusPass:
			t.Errorf("step %s = %s (%s)", r.ID, r.Status, r.Message)
		}
	}
	if !page.elems[`[data-testid="contact-save"]`].clicked {
		t.Error("the run did not continue to Save")
	}
	if page.fpCalls != 1 || len(page.marked) != 0 {
		t.Errorf("fingerprint calls = %d, markers left = %v", page.fpCalls, page.marked)
	}
}

// Without --self-heal the picker is never asked: the step fails cleanly
// and the rest is skipped.
func TestRunReplayWithoutSelfHealNeverAsksJev(t *testing.T) {
	srv := jevtest.NewServer(t, jevtest.Fixed(map[string]string{"target": "1"}))
	plan := formPlan(t, map[string]any{"account_password": "pw"})
	step := fullNameStep(t, plan)
	page := newJevFormPage()
	page.hideFullName()
	rep := RunReplay(context.Background(), plan, ReplayRunOptions{Page: page, Logger: zerolog.Nop(),
		Confirm: func(ReplayStep) bool { return true }})

	if srv.Calls() != 0 {
		t.Fatalf("jev asked %d times without --self-heal", srv.Calls())
	}
	if rep.OK || rep.Error == "" || len(rep.SelfHealed) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	seen := false
	for _, r := range rep.Steps {
		switch {
		case r.ID == step.ID:
			seen = true
			if r.Status != StatusFail {
				t.Errorf("full-name row = %+v", r)
			}
		case seen && r.Status != StatusSkipped:
			t.Errorf("step %s after the failure = %s", r.ID, r.Status)
		}
	}
	if page.elems[`[data-testid="contact-save"]`].clicked {
		t.Error("Save ran after a failed step")
	}
}

// On the happy path the picker is installed but never asked.
func TestRunReplaySelfHealIdleOnHappyPath(t *testing.T) {
	srv := jevtest.NewServer(t, jevtest.Fixed(map[string]string{"target": "1"}))
	plan := formPlan(t, map[string]any{"account_password": "pw"})
	page := newJevFormPage()
	rep := RunReplay(context.Background(), plan, ReplayRunOptions{Page: page, Logger: zerolog.Nop(),
		SelfHeal: jevSelfHeal(t), Confirm: func(ReplayStep) bool { return true }})

	if !rep.OK || len(rep.SelfHealed) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if srv.Calls() != 0 || page.fpCalls != 0 {
		t.Errorf("jev calls = %d, fingerprint calls = %d on the happy path", srv.Calls(), page.fpCalls)
	}
	for _, r := range rep.Steps {
		if r.Status != StatusPass {
			t.Errorf("step %s = %s (%s)", r.ID, r.Status, r.Message)
		}
	}
}
