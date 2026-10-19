package evaluation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

// NodeType is the Evaluation node's type (internal/nodes/evalnode).
const NodeType = "core.evaluation"

// Metric names core.evaluation's setMetrics computes.
const (
	MetricCorrectness      = "correctness"
	MetricHelpfulness      = "helpfulness"
	MetricStringSimilarity = "stringSimilarity"
	MetricCategorization   = "categorization"
	MetricToolsUsed        = "toolsUsed"
	MetricCustom           = "customMetrics"
	// DefaultMetric is the metric a setMetrics node without one computes,
	// as in n8n.
	DefaultMetric = MetricCorrectness
)

// NeedsModel reports whether a metric is judged by a model.
func NeedsModel(metric string) bool {
	return metric == MetricCorrectness || metric == MetricHelpfulness
}

// Engine is what a run needs of the workflow engine.
type Engine interface {
	GetWorkflow(ctx context.Context, id string) (*workflow.Workflow, error)
	StartEvaluationCase(ctx context.Context, workflowID, triggerNodeID string, row map[string]interface{}, c workflow.EvaluationCase) (string, error)
	GetExecution(ctx context.Context, id string) (*workflow.WorkflowExecution, error)
	ExecutionInFlight(ctx context.Context, id string) (status string, inFlight bool, err error)
	CancelExecution(executionID string)
	ResolveCredentials(ctx context.Context, nodeType string, cfg map[string]interface{}) map[string]interface{}
}

// Options tune a run.
type Options struct {
	// Table, when set, is the data table the rows come from instead of the
	// one the trigger names (`workflow evaluate --dataset`).
	Table string
	// CaseTimeout bounds the wait for each case's execution (0: 15 minutes).
	CaseTimeout time.Duration
	// Progress, when set, is called after each case finishes.
	Progress func(Case)
}

// pollInterval is how often a case's execution status is read.
var pollInterval = 200 * time.Millisecond

// Execute runs workflowID once per dataset row and returns the finished
// run with its cases. A run that cannot start (no trigger.evaluation, no
// metrics node, no rows…) is stored and returned with status "error" and
// its error code; err is reserved for failures to reach the workflow or
// the database.
func Execute(ctx context.Context, db *sql.DB, profile string, eng Engine, workflowID string, opts Options) (*Run, error) {
	wf, err := eng.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	if wf == nil {
		return nil, fmt.Errorf("evaluation: %w", workflow.ErrWorkflowNotFound)
	}
	store := NewStore(db, profile)
	trigger, code, msg := checkWorkflow(wf)
	var ds Dataset
	if code == "" {
		cfg := map[string]interface{}{}
		for k, v := range trigger.Config {
			cfg[k] = v
		}
		if opts.Table != "" {
			cfg["source"], cfg["table"] = SourceDataTable, opts.Table
		}
		if ds, err = ParseDataset(cfg); err != nil {
			code, msg = ErrTriggerNotConfigured, err.Error()
		}
	}
	run, err := store.CreateRun(ctx, workflowID, ds.Describe())
	if err != nil {
		return nil, err
	}
	fail := func(code, msg string) (*Run, error) {
		run.Status, run.ErrorCode, run.ErrorMessage = RunError, code, msg
		if err := store.FinishRun(context.WithoutCancel(ctx), run); err != nil {
			return nil, err
		}
		return store.GetRun(context.WithoutCancel(ctx), run.ID)
	}
	if code != "" {
		return fail(code, msg)
	}
	rows, err := ds.LoadRows(ctx, db, profile, eng.ResolveCredentials)
	if err != nil {
		return fail(ErrCantFetchTestCases, err.Error())
	}
	if len(rows) == 0 {
		return fail(ErrTestCasesNotFound, "the dataset has no rows to run")
	}
	timeout := opts.CaseTimeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	sums, counts := map[string]float64{}, map[string]int{}
	for i, row := range rows {
		c := &Case{Position: i, RowID: row.ID, RowNumber: row.Number}
		if err := store.AddCase(ctx, run.ID, c); err != nil {
			return nil, err
		}
		if ctx.Err() == nil {
			runCase(ctx, store, eng, wf.ID, trigger.ID, run.ID, row, c, timeout)
		} else {
			c.Status, c.ErrorCode, c.ErrorMessage = CaseCancelled, ErrInterrupted, "the run was stopped"
		}
		if err := store.UpdateCase(context.WithoutCancel(ctx), c, true); err != nil {
			return nil, err
		}
		if c.Status == CaseSuccess || c.ErrorCode == ErrNoMetricsCollected {
			for name, v := range c.Metrics {
				if f, ok := v.(float64); ok {
					sums[name] += f
					counts[name]++
				}
			}
		}
		if opts.Progress != nil {
			opts.Progress(*c)
		}
	}
	for name, sum := range sums {
		run.Metrics[name] = sum / float64(counts[name])
	}
	run.Status = RunCompleted
	if ctx.Err() != nil {
		run.Status, run.ErrorCode, run.ErrorMessage = RunCancelled, ErrInterrupted, "the run was stopped"
	}
	if err := store.FinishRun(context.WithoutCancel(ctx), run); err != nil {
		return nil, err
	}
	return store.GetRun(context.WithoutCancel(ctx), run.ID)
}

// checkWorkflow finds the workflow's trigger.evaluation node and checks it
// has a configured setMetrics node, as n8n does before a test run.
func checkWorkflow(wf *workflow.Workflow) (trigger workflow.WorkflowNode, code, msg string) {
	found := false
	for _, n := range wf.Nodes {
		if n.Type == workflow.TriggerTypeEvaluation {
			trigger, found = n, true
			break
		}
	}
	if !found {
		return trigger, ErrTriggerNotFound, "the workflow has no evaluation trigger (trigger.evaluation)"
	}
	if trigger.Disabled {
		return trigger, ErrTriggerDisabled, fmt.Sprintf("the evaluation trigger %q is disabled", trigger.Name)
	}
	models := map[string]bool{}
	for _, c := range wf.Connections {
		if c.SourceHandle == "ai_languageModel" || c.TargetHandle == "ai_languageModel" {
			models[c.TargetNodeID] = true
		}
	}
	metrics := 0
	for _, n := range wf.Nodes {
		if n.Type != NodeType || n.Disabled {
			continue
		}
		cfg := n.Config
		switch op, _ := cfg["operation"].(string); op {
		case "setMetrics":
			metrics++
			metric, _ := cfg["metric"].(string)
			if metric == "" {
				metric = DefaultMetric
			}
			if metric == MetricCustom && !hasEntries(cfg["metrics"]) {
				return trigger, ErrMetricsNotConfigured, fmt.Sprintf("%q has no metrics to return", n.Name)
			}
			if rt, _ := cfg["runtime"].(string); NeedsModel(metric) && strings.TrimSpace(rt) == "" && !models[n.ID] {
				return trigger, ErrMetricsNotConfigured, fmt.Sprintf("%q judges %s with a model: set its runtime or attach a chat model", n.Name, metric)
			}
		case "setOutputs":
			if !hasEntries(cfg["outputs"]) {
				return trigger, ErrOutputsNotConfigured, fmt.Sprintf("%q has no outputs to set", n.Name)
			}
		}
	}
	if metrics == 0 {
		return trigger, ErrMetricsNodeNotFound, "the workflow has no core.evaluation node with operation setMetrics"
	}
	return trigger, "", ""
}

// hasEntries reports whether a list config (or its JSON text) has items.
func hasEntries(v interface{}) bool {
	switch l := v.(type) {
	case []interface{}:
		return len(l) > 0
	case string:
		s := strings.TrimSpace(l)
		return s != "" && s != "[]"
	}
	return false
}

// runCase runs one row and records its outcome in c.
func runCase(ctx context.Context, store *Store, eng Engine, workflowID, triggerID, runID string, row Row, c *Case, timeout time.Duration) {
	c.Status = CaseRunning
	execID, err := eng.StartEvaluationCase(ctx, workflowID, triggerID, row.JSON,
		workflow.EvaluationCase{RunID: runID, CaseID: c.ID, RowID: row.ID, RowNumber: row.Number, Table: row.Table})
	c.ExecutionID = execID
	if err != nil {
		c.Status, c.ErrorCode, c.ErrorMessage = CaseError, ErrFailedToExecute, err.Error()
		return
	}
	_ = store.UpdateCase(ctx, c, false)
	exec, err := waitFor(ctx, eng, execID, timeout)
	if err != nil {
		eng.CancelExecution(execID)
		c.Status, c.ErrorCode, c.ErrorMessage = CaseError, ErrFailedToExecute, err.Error()
		if ctx.Err() != nil {
			c.Status, c.ErrorCode, c.ErrorMessage = CaseCancelled, ErrInterrupted, "the run was stopped"
		}
		return
	}
	if err := store.LoadCase(context.WithoutCancel(ctx), c); err != nil {
		c.Status, c.ErrorCode, c.ErrorMessage = CaseError, ErrUnknown, err.Error()
		return
	}
	switch exec.Status {
	case "SUCCESS", "SUCCESS_WITH_ERRORS":
	case "WAITING":
		eng.CancelExecution(execID)
		c.Status, c.ErrorCode = CaseError, ErrFailedToExecute
		c.ErrorMessage = "the execution paused for a person (human review or a reply); an evaluation cannot wait for one"
		return
	default:
		c.Status, c.ErrorCode = CaseError, ErrFailedToExecute
		c.ErrorMessage = fmt.Sprintf("execution %s ended %s", execID, exec.Status)
		if exec.ErrorMessage != "" {
			c.ErrorMessage += ": " + exec.ErrorMessage
		}
		return
	}
	user := len(c.Metrics)
	if bad := invalidMetrics(c.Metrics); bad != "" {
		c.Status, c.ErrorCode, c.ErrorMessage = CaseError, ErrInvalidMetrics, bad
		return
	}
	if exec.StartedAt != nil && exec.FinishedAt != nil {
		c.Metrics["executionTime"] = float64(exec.FinishedAt.Sub(*exec.StartedAt).Milliseconds())
	}
	if user == 0 {
		c.Status, c.ErrorCode, c.ErrorMessage = CaseError, ErrNoMetricsCollected, "no setMetrics node ran in this case"
		return
	}
	c.Status = CaseSuccess
}

// invalidMetrics names the first metric (by name) that is not a number.
func invalidMetrics(m map[string]interface{}) string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if _, ok := m[k].(float64); !ok {
			return fmt.Sprintf("metric %q is %v, not a number", k, m[k])
		}
	}
	return ""
}

// waitFor polls an execution until it is no longer queued or running (or
// waiting only for its sub-workflows) and returns its record.
func waitFor(ctx context.Context, eng Engine, execID string, timeout time.Duration) (*workflow.WorkflowExecution, error) {
	deadline := time.Now().Add(timeout)
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.C:
		}
		_, inFlight, err := eng.ExecutionInFlight(ctx, execID)
		if err != nil {
			return nil, err
		}
		if !inFlight {
			return eng.GetExecution(ctx, execID)
		}
		if time.Now().After(deadline) {
			return nil, errors.New("timed out waiting for execution " + execID)
		}
	}
}
