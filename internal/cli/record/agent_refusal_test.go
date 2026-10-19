package record

import (
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/action"
	"github.com/monoes/mono-agent/internal/recordanalyze"
)

// asAgent makes the commands' operator check refuse, as from an agent's
// shell, until the test ends.
func asAgent(t *testing.T) {
	t.Helper()
	prev := seams.Refusal
	seams.Refusal = func() string { return "CLAUDECODE is set, so an agent is running this command" }
	t.Cleanup(func() { seams.Refusal = prev })
}

func operatorOnlyCode(t *testing.T, out string) bool {
	t.Helper()
	var body map[string]any
	return json.Unmarshal([]byte(out), &body) == nil && body["code"] == "operator_only"
}

// A replay runs in the user's logged-in browser, and which of its steps
// change a site is read from the recording, a file an agent can write: any
// replay of an agent's is refused before the browser opens (exit 3,
// operator_only), with --yes or without. Its --dry-run, which runs nothing,
// stays open; the user's replay reaches the browser.
func TestRecordReplayIsTheUsers(t *testing.T) {
	ingestFormRecording(t, "rec-yes")
	stubReplayBrowser(t, nil)
	asAgent(t)
	for _, args := range [][]string{
		{"replay", "rec-yes", "--yes", "--input", "account_password=pw"},
		{"replay", "rec-yes", "--input", "account_password=pw"},
	} {
		out, err := runRecordCLI(t, true, args...)
		if err == nil || strings.Contains(err.Error(), "stub") || !operatorOnlyCode(t, out) {
			t.Fatalf("an agent's %v: err=%v out=%s, want operator_only before the browser", args, err, out)
		}
	}
	for _, args := range [][]string{{"replay", "rec-yes", "--dry-run"}, {"replay", "rec-yes", "--yes", "--dry-run"}} {
		if out, err := runRecordCLI(t, true, args...); err != nil {
			t.Fatalf("an agent's %v: %v\n%s", args, err, out)
		}
	}
	seams.Refusal = func() string { return "" }
	if _, err := runRecordCLI(t, false, "replay", "rec-yes", "--input", "account_password=pw"); err == nil || !strings.Contains(err.Error(), "stub") {
		t.Fatalf("the user's replay: %v, want it to reach the browser", err)
	}
}

// Without --yes, a replay asks before each step that changes the site, and
// only the user can answer: an agent's replay is never asked, even with a
// terminal, so it stops before the first such step, as one with no
// terminal does.
func TestRecordReplayNeverAsksAnAgent(t *testing.T) {
	d := newTestDeps(t, false)
	d.StdinIsTerminal = func() bool { return true }
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetErr(io.Discard)
	if d.replayConfirm(cmd, false, "an agent is running this command") != nil {
		t.Error("an agent with a terminal is asked to confirm a step that changes the site")
	}
	if c := d.replayConfirm(cmd, false, ""); c == nil || !c(recordanalyze.ReplayStep{ID: "s5", Describe: "click Send"}) {
		t.Error("the user's terminal is not asked, or their yes is not taken")
	}
	d.StdinIsTerminal = func() bool { return false }
	if d.replayConfirm(cmd, false, "") != nil {
		t.Error("a replay with no terminal is confirmed")
	}
	if c := d.replayConfirm(cmd, true, ""); c == nil || !c(recordanalyze.ReplayStep{}) {
		t.Error("the user's --yes does not run the step")
	}
}

// `record verify` runs a draft's steps in the user's logged-in browser, and
// which of them change a site is read from the draft: an agent is refused
// any verify, with --full or without, before the browser. The user's verify
// runs safe, --full runs full, and --stop-at-first-action (the extension's)
// reaches the replay with its context marked.
func TestRecordVerifyIsTheUsers(t *testing.T) {
	dir := analyzeRec1(t)
	type replay struct{ safe, stopAtActions bool }
	var calls []replay
	prev := seams.VerifyExec
	seams.VerifyExec = func(context.Context, string, bool, bool, func(string) (string, bool)) (recordanalyze.ExecFunc, error) {
		return func(ctx context.Context, _ *action.ActionDef, _ action.PackageContext, _ map[string]any, safe bool, _ action.SelectorObserver) recordanalyze.RunOutcome {
			calls = append(calls, replay{safe, recordanalyze.StopsAtActions(ctx)})
			return recordanalyze.RunOutcome{Result: &action.ExecutionResult{}}
		}, nil
	}
	t.Cleanup(func() { seams.VerifyExec = prev })

	asAgent(t)
	for _, args := range [][]string{{"verify", dir}, {"verify", dir, "--full"}, {"verify", dir, "--stop-at-first-action"}} {
		out, err := runRecordCLI(t, true, args...)
		if err == nil || !operatorOnlyCode(t, out) || len(calls) != 0 {
			t.Fatalf("an agent's %v: err=%v out=%s replays=%v, want operator_only before the browser", args, err, out, calls)
		}
	}
	seams.Refusal = func() string { return "" }
	_, _ = runRecordCLI(t, true, "verify", dir)
	_, _ = runRecordCLI(t, true, "verify", dir, "--full")
	_, _ = runRecordCLI(t, true, "verify", dir, "--stop-at-first-action")
	want := []replay{{true, false}, {false, false}, {true, true}}
	if !slices.Equal(calls, want) {
		t.Fatalf("the user's replays = %v, want %v", calls, want)
	}
	if _, err := runRecordCLI(t, true, "verify", dir, "--full", "--stop-at-first-action"); err == nil {
		t.Fatal("--full with --stop-at-first-action was accepted")
	}
}
