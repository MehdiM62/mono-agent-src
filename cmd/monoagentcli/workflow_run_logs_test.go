package main

import (
	"bytes"
	"encoding/json"
	"log"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/logging"
	"github.com/monoes/mono-agent/internal/workflow"
)

// failingRunWorkflow builds and activates a workflow whose aggregate node has
// no operation, so it fails when it runs.
func failingRunWorkflow(t *testing.T, cfg *globalConfig) string {
	t.Helper()
	var created workflow.Workflow
	if err := json.Unmarshal([]byte(runWorkflowSubcmd(t, cfg, "create", "failing-run")), &created); err != nil {
		t.Fatal(err)
	}
	add := func(typ, name string) string {
		var n workflow.WorkflowNode
		if err := json.Unmarshal([]byte(runWorkflowSubcmd(t, cfg, "node", "add", created.ID, "--type", typ, "--name", name)), &n); err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	trig := add("trigger.manual", "Start")
	agg := add("core.aggregate", "Count")
	runWorkflowSubcmd(t, cfg, "connect", created.ID, "--from", trig, "--to", agg)
	runWorkflowSubcmd(t, cfg, "activate", created.ID)
	return created.ID
}

// runLogsFor runs workflowID with the root logger initialised from args (as
// main does) and returns what it logged and printed.
func runLogsFor(t *testing.T, cfg *globalConfig, workflowID string, args ...string) (logs, stdout string) {
	t.Helper()
	t.Setenv(logging.EnvLevel, "")
	prevDefault, prevOut, prevFlags, prevLevel := slog.Default(), log.Writer(), log.Flags(), logging.Level()
	var buf bytes.Buffer
	logging.Init(args, func(string) string { return "" }, &buf)
	t.Cleanup(func() {
		logging.Init(nil, func(string) string { return "" }, os.Stderr)
		slog.SetDefault(prevDefault)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		logging.SetLevel(prevLevel)
	})
	cmd := newWorkflowCmd(cfg)
	cmd.SetArgs([]string{"run", workflowID, "--timeout", "30s"})
	stdout = captureStdout(t, func() { _ = cmd.Execute() })
	// Undo the run's demotion (a CLI process runs one command).
	logging.Demote(slog.LevelInfo, oneShotRunLogs...)()
	return buf.String(), stdout
}

// A human-mode run's summary reports a failed node; the engine's own log
// lines about it stay off the terminal unless a level was chosen.
func TestWorkflowRunHumanModeQuietsFailureLogs(t *testing.T) {
	_, cfg := persistTestEnv(t)
	id := failingRunWorkflow(t, cfg)
	cfg.JSONOutput = false

	logs, stdout := runLogsFor(t, cfg, id)
	if strings.Contains(logs, "level=") {
		t.Errorf("human-mode run logged:\n%s", logs)
	}
	if !strings.Contains(stdout, "FAILED") {
		t.Errorf("summary does not report the failure:\n%s", stdout)
	}

	logs, _ = runLogsFor(t, cfg, id, "--log-level=warn")
	if !strings.Contains(logs, `msg="node execution failed"`) {
		t.Errorf("--log-level=warn hid the node failure:\n%s", logs)
	}
}

// --json output is unchanged: the record on stdout, the engine's lines on
// stderr as before.
func TestWorkflowRunJSONKeepsFailureLogs(t *testing.T) {
	_, cfg := persistTestEnv(t)
	id := failingRunWorkflow(t, cfg)

	logs, stdout := runLogsFor(t, cfg, id)
	var rec struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(stdout), &rec); err != nil || rec.Status != "FAILED" {
		t.Errorf("run --json = %q (%v)", stdout, err)
	}
	if !strings.Contains(logs, `msg="node execution failed"`) {
		t.Errorf("--json run lost the engine's lines:\n%s", logs)
	}
}
