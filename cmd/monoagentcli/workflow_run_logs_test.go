package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/cli/clicore"
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
func runLogsFor(t *testing.T, cfg *globalConfig, workflowID string, args ...string) (logs, stdout string, err error) {
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
	stdout = captureStdout(t, func() { err = cmd.Execute() })
	// Undo the run's demotion (a CLI process runs one command).
	logging.Demote(slog.LevelInfo, oneShotRunLogs...)()
	return buf.String(), stdout, err
}

// A human-mode run's summary reports a failed node; the engine's own log
// lines about it stay off the terminal unless a level was chosen.
func TestWorkflowRunHumanModeQuietsFailureLogs(t *testing.T) {
	_, cfg := persistTestEnv(t)
	id := failingRunWorkflow(t, cfg)
	cfg.JSONOutput = false

	logs, stdout, err := runLogsFor(t, cfg, id)
	if strings.Contains(logs, "level=") {
		t.Errorf("human-mode run logged:\n%s", logs)
	}
	if !strings.Contains(stdout, "Status: FAILED") || !strings.Contains(stdout, "Error:  node") {
		t.Errorf("summary does not report the failure:\n%s", stdout)
	}
	// The summary showed the error: main exits 1 without printing it again.
	var stdoutDoc, stderr bytes.Buffer
	reportCommandError([]string{"workflow", "run", id}, err, &stdoutDoc, &stderr)
	if err == nil || exitCodeFor(err) != 1 || stderr.Len() != 0 || stdoutDoc.Len() != 0 {
		t.Errorf("err %v (exit %d) reported as stderr %q stdout %q", err, exitCodeFor(err), stderr.String(), stdoutDoc.String())
	}

	logs, _, _ = runLogsFor(t, cfg, id, "--log-level=warn")
	if !strings.Contains(logs, `msg="node execution failed"`) {
		t.Errorf("--log-level=warn hid the node failure:\n%s", logs)
	}
}

// --json output is unchanged: the record on stdout, the engine's lines on
// stderr as before.
func TestWorkflowRunJSONKeepsFailureLogs(t *testing.T) {
	_, cfg := persistTestEnv(t)
	id := failingRunWorkflow(t, cfg)

	logs, stdout, err := runLogsFor(t, cfg, id)
	var rec struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(stdout), &rec); err != nil || rec.Status != "FAILED" {
		t.Errorf("run --json = %q (%v)", stdout, err)
	}
	if !strings.Contains(logs, `msg="node execution failed"`) {
		t.Errorf("--json run lost the engine's lines:\n%s", logs)
	}
	// --json reports the failure as before: the record above is the
	// document, the error goes to stderr, exit 1.
	var doc, stderr bytes.Buffer
	reportCommandError([]string{"--json", "workflow", "run", id}, err, &doc, &stderr)
	if errors.As(err, new(clicore.Shown)) || exitCodeFor(err) != 1 || !strings.Contains(stderr.String(), "failed: ") {
		t.Errorf("--json error %v: stdout %q stderr %q", err, doc.String(), stderr.String())
	}
}

// A wrapper that adds to a shown failure (a hint) is still printed.
func TestReportCommandErrorShownWithHint(t *testing.T) {
	shown := clicore.Shown{Err: errors.New("execution e1 failed: boom")}
	var stdout, stderr bytes.Buffer
	reportCommandError(nil, fmt.Errorf("%w — install it", shown), &stdout, &stderr)
	if !strings.Contains(stderr.String(), "boom — install it") {
		t.Errorf("stderr = %q", stderr.String())
	}
}
