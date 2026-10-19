package org

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/orgdecide"
	"github.com/monoes/mono-agent/internal/orgdesign"
)

// stubRuntimes points checkDeciderModel at a fixed catalog instead of a real
// runtime binary, and restores the real ones afterwards.
func stubRuntimes(t *testing.T, entries []monomind.ScanEntry, models []monomind.RuntimeModel, listErr error) {
	t.Helper()
	oldScan, oldList := scanAgentRuntimes, listRuntimeModels
	scanAgentRuntimes = func(context.Context) (*monomind.ScanResult, error) {
		return &monomind.ScanResult{Agents: entries}, nil
	}
	listRuntimeModels = func(context.Context, string, string) ([]monomind.RuntimeModel, error) {
		return models, listErr
	}
	t.Cleanup(func() { scanAgentRuntimes, listRuntimeModels = oldScan, oldList })
}

func codexInstalled() []monomind.ScanEntry {
	bin := "/usr/bin/codex"
	return []monomind.ScanEntry{{ID: "codex", Installed: true, Binary: &bin}}
}

func modelDecider(runtime, model string) *orgdecide.Autonomy {
	a := &orgdecide.Autonomy{}
	a.Decider.Kind = orgdesign.DeciderModel
	a.Decider.Runtime = runtime
	a.Decider.Model = model
	return a
}

// A wrong model name used to be accepted in silence and only showed up
// mid-run as "decider failed: … runner-error: done reported nonzero exit_code
// 1" — which reads like a broken decider rather than a model that does not
// exist. ("gpt-5" is real elsewhere but not offered to a codex ChatGPT
// account, so a plausible name is exactly the trap.)
func TestCheckDeciderModelRejectsAModelTheRuntimeDoesNotOffer(t *testing.T) {
	stubRuntimes(t, codexInstalled(), []monomind.RuntimeModel{
		{ID: "gpt-6-astra"}, {ID: "gpt-5.6-luna"}, {ID: "gpt-5.5"},
	}, nil)

	err := depsEnv().checkDeciderModel(context.Background(), modelDecider("codex", "gpt-5"))
	if err == nil {
		t.Fatal("a model the runtime does not offer must be refused")
	}
	// The message has to carry the way out, not just the refusal.
	for _, want := range []string{`"gpt-5"`, "codex", "gpt-6-astra", "gpt-5.6-luna", "gpt-5.5"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q is missing %q", err, want)
		}
	}
}

func TestCheckDeciderModelAcceptsAnOfferedModel(t *testing.T) {
	stubRuntimes(t, codexInstalled(), []monomind.RuntimeModel{{ID: "gpt-5.6-luna"}}, nil)
	if err := depsEnv().checkDeciderModel(context.Background(), modelDecider("codex", "gpt-5.6-luna")); err != nil {
		t.Fatalf("an offered model must pass: %v", err)
	}
}

// Claude lists its models by alias, each with the model it resolves to today
// ({"id":"fable","resolved_id":"claude-fable-5-1"}), and the default decider
// model is the concrete id. Matching ids alone refused it, so even turning an
// org to manual failed (#477).
func TestCheckDeciderModelAcceptsTheModelAnAliasResolvesTo(t *testing.T) {
	bin := "/usr/bin/claude"
	stubRuntimes(t, []monomind.ScanEntry{{ID: "claude", Installed: true, Binary: &bin}}, []monomind.RuntimeModel{
		{ID: "default", ResolvedID: "claude-sonnet-5-5"}, {ID: "fable", ResolvedID: "claude-fable-5-1"}, {ID: "claude-opus-5"},
	}, nil)
	for _, model := range []string{orgdecide.DefaultDeciderModel, "fable", "claude-opus-5"} {
		if err := depsEnv().checkDeciderModel(context.Background(), modelDecider("claude", model)); err != nil {
			t.Errorf("%s: %v", model, err)
		}
	}
	if err := depsEnv().checkDeciderModel(context.Background(), modelDecider("claude", "claude-opus-9")); err == nil {
		t.Error("a model no entry names or resolves to must still be refused")
	}
}

func TestCheckDeciderModelRejectsAnUninstalledRuntime(t *testing.T) {
	stubRuntimes(t, []monomind.ScanEntry{{ID: "codex", Installed: false, InstallHint: "npm i -g codex"}}, nil, nil)
	err := depsEnv().checkDeciderModel(context.Background(), modelDecider("codex", "gpt-5.6-luna"))
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("want a not-installed error, got %v", err)
	}
	if !strings.Contains(err.Error(), "npm i -g codex") {
		t.Errorf("the install hint should be passed on: %v", err)
	}
}

// Fail open on our own inability to look: a runtime with no discovery
// command, a listing that errors, or an empty catalog must leave the model as
// typed rather than block a legitimate configuration.
func TestCheckDeciderModelLeavesTheModelAloneWhenItCannotCheck(t *testing.T) {
	cases := []struct {
		name    string
		models  []monomind.RuntimeModel
		listErr error
		runtime string
		model   string
	}{
		{name: "listing errored", listErr: errors.New("codex debug models: exit 1"), runtime: "codex", model: "anything"},
		{name: "no catalog for this runtime", models: nil, runtime: "claude", model: "some-model"},
		{name: "empty catalog", models: []monomind.RuntimeModel{}, runtime: "codex", model: "some-model"},
		{name: "no model set", models: []monomind.RuntimeModel{{ID: "x"}}, runtime: "codex", model: ""},
		{name: "no runtime set", models: []monomind.RuntimeModel{{ID: "x"}}, runtime: "", model: "some-model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubRuntimes(t, codexInstalled(), tc.models, tc.listErr)
			if err := depsEnv().checkDeciderModel(context.Background(), modelDecider(tc.runtime, tc.model)); err != nil {
				t.Fatalf("should not block: %v", err)
			}
		})
	}
}

// "Answer this or the org stops in N seconds" is the difference between a
// pending item and a deadline, and needs-you reported idle_stop_in_seconds as
// null for every item because nothing ever filled it in. monomind publishes
// it in `org status --json` under the org-idle-deadline capability.
func TestIdleDeadlineReadsWhatMonomindReports(t *testing.T) {
	cases := []struct {
		name        string
		status      string
		statusErr   error
		wantSeconds interface{}
		wantHold    interface{}
	}{
		{
			name:        "a running org counting down",
			status:      `{"status":"running","idle_stop_at":"2026-09-19T11:00:00Z","idle_stop_in_seconds":42,"idle_hold":null}`,
			wantSeconds: float64(42),
		},
		{
			// A legitimate wait reports what is holding the watchdog off and
			// no deadline — exactly what a person needs to see.
			name:     "held open by a pending approval",
			status:   `{"status":"running","idle_stop_at":null,"idle_stop_in_seconds":null,"idle_hold":"pending-approval"}`,
			wantHold: "pending-approval",
		},
		{
			name:   "monomind older than the capability says nothing",
			status: `{"status":"running","run":"run-1"}`,
		},
		{
			name:   "not running",
			status: `{"status":"stopped"}`,
		},
		{name: "status unreadable", status: `not json`},
		{name: "status failed", statusErr: errors.New("monomind org status: exit 1")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			old := orgStatusJSON
			orgStatusJSON = func(context.Context, string, string) (json.RawMessage, error) {
				return json.RawMessage(tc.status), tc.statusErr
			}
			t.Cleanup(func() { orgStatusJSON = old })

			seconds, hold := idleDeadline(context.Background(), "/root", "growth")
			if seconds != tc.wantSeconds {
				t.Errorf("seconds = %v (%T), want %v", seconds, seconds, tc.wantSeconds)
			}
			if hold != tc.wantHold {
				t.Errorf("hold = %v, want %v", hold, tc.wantHold)
			}
		})
	}
}

// Turning an org down to manual uses no decider, so a stored decider the
// runtime no longer offers must not block it (#477); raising the level, or
// changing the decider, still checks it.
func TestOrgAutonomyManualDoesNotNeedTheDecider(t *testing.T) {
	f := newOrgCLIFixture(t)
	bin := "/usr/bin/claude"
	stubRuntimes(t, []monomind.ScanEntry{{ID: "claude", Installed: true, Binary: &bin}}, []monomind.RuntimeModel{{ID: "opus"}}, nil)

	got := f.mustRun(t, "autonomy", "set", "growth", "--level", "manual")
	if got["level"] != orgdesign.LevelManual {
		t.Fatalf("level = %v", got["level"])
	}
	if _, err := f.run(t, "autonomy", "set", "growth", "--level", "mid"); err == nil || !strings.Contains(err.Error(), "is not one claude offers") {
		t.Fatalf("raising the level with a model claude does not offer: %v", err)
	}
	if _, err := f.run(t, "autonomy", "set", "growth", "--level", "manual", "--decider-model", "claude-opus-9"); err == nil {
		t.Fatal("a decider changed on the way to manual is still checked")
	}
}
