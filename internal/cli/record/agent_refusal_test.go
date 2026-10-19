package record

import (
	"context"
	"encoding/json"
	"io"
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

// `record replay --yes` runs a recording's steps that change a site without
// asking, so it is the user's: an agent is refused before the browser opens
// (exit 3, operator_only). An agent could replay any recording, its own or
// the user's, in the user's logged-in browser, posting or sending with no
// approval. Its --dry-run, which runs nothing, stays open.
func TestRecordReplayYesIsTheUsers(t *testing.T) {
	ingestFormRecording(t, "rec-yes")
	stubReplayBrowser(t, nil)
	asAgent(t)
	out, err := runRecordCLI(t, true, "replay", "rec-yes", "--yes", "--input", "account_password=pw")
	if err == nil || strings.Contains(err.Error(), "stub") || !operatorOnlyCode(t, out) {
		t.Fatalf("an agent's replay --yes: err=%v out=%s, want operator_only before the browser", err, out)
	}
	if out, err := runRecordCLI(t, true, "replay", "rec-yes", "--yes", "--dry-run"); err != nil {
		t.Fatalf("an agent's --dry-run --yes: %v\n%s", err, out)
	}
	seams.Refusal = func() string { return "" }
	if _, err := runRecordCLI(t, false, "replay", "rec-yes", "--yes", "--input", "account_password=pw"); err == nil || !strings.Contains(err.Error(), "stub") {
		t.Fatalf("the user's replay --yes: %v, want it to reach the browser", err)
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

// `record verify` replays a draft in safe mode, stopping before the first
// step that changes the site, and stays open to anyone; --full runs those
// steps too, so it is the user's: an agent is refused before the browser.
func TestRecordVerifyFullIsTheUsers(t *testing.T) {
	dir := analyzeRec1(t)
	var calls []bool // safe, per replay
	prev := seams.VerifyExec
	seams.VerifyExec = func(context.Context, string, bool, bool, func(string) (string, bool)) (recordanalyze.ExecFunc, error) {
		return func(_ context.Context, _ *action.ActionDef, _ action.PackageContext, _ map[string]any, safe bool, _ action.SelectorObserver) recordanalyze.RunOutcome {
			calls = append(calls, safe)
			return recordanalyze.RunOutcome{Result: &action.ExecutionResult{}}
		}, nil
	}
	t.Cleanup(func() { seams.VerifyExec = prev })

	asAgent(t)
	out, err := runRecordCLI(t, true, "verify", dir, "--full")
	if err == nil || !operatorOnlyCode(t, out) || len(calls) != 0 {
		t.Fatalf("an agent's verify --full: err=%v out=%s replays=%v, want operator_only before the browser", err, out, calls)
	}
	_, _ = runRecordCLI(t, true, "verify", dir)
	if len(calls) != 1 || !calls[0] {
		t.Fatalf("an agent's verify: replays=%v, want one safe replay", calls)
	}
	seams.Refusal = func() string { return "" }
	_, _ = runRecordCLI(t, true, "verify", dir, "--full")
	if len(calls) != 2 || calls[1] {
		t.Fatalf("the user's verify --full: replays=%v, want a full one", calls)
	}
}
