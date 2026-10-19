package evaluation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/testdb"
	"github.com/monoes/mono-agent/internal/testhome"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Tests never touch the real ~/.monoagent.
func TestMain(m *testing.M) { testhome.Main(m) }

// A Google Sheet dataset is read through the Sheets node with the
// trigger's credential resolved: each row's number is its sheet row, the
// eq filters compare as text, and the limit counts matching rows.
func TestSheetDataset(t *testing.T) {
	var got map[string]interface{}
	old := readSheet
	readSheet = func(_ context.Context, cfg map[string]interface{}) ([]workflow.Item, error) {
		got = cfg
		return []workflow.Item{
			{JSON: map[string]interface{}{"q": "a", "split": "train", "_row_index": 2, "_row_range": "A2:B2"}},
			{JSON: map[string]interface{}{"q": "b", "split": "test", "_row_index": 3}},
			{JSON: map[string]interface{}{"q": "c", "split": "test", "_row_index": 5}},
			{JSON: map[string]interface{}{"q": "d", "split": "test", "_row_index": 6}},
		}, nil
	}
	t.Cleanup(func() { readSheet = old })
	ds, err := ParseDataset(map[string]interface{}{"source": "googleSheets", "credential_id": "g1", "spreadsheet_id": "S",
		"sheet_name": "Cases", "limit_rows": true, "max_rows": 2.0,
		"filters": []interface{}{map[string]interface{}{"column": "split", "value": "test"}}})
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(_ context.Context, nodeType string, cfg map[string]interface{}) map[string]interface{} {
		if nodeType != "service.google_sheets" || cfg["credential_id"] != "g1" {
			t.Errorf("resolve %s %v", nodeType, cfg)
		}
		out := map[string]interface{}{"access_token": "tok"}
		for k, v := range cfg {
			out[k] = v
		}
		return out
	}
	rows, err := ds.LoadRows(context.Background(), nil, "default", resolve)
	if err != nil {
		t.Fatal(err)
	}
	if got["access_token"] != "tok" || got["operation"] != "read_rows" || got["sheet_name"] != "Cases" || got["use_header_row"] != true {
		t.Errorf("sheets call: %v", got)
	}
	if len(rows) != 2 || rows[0].Number != 3 || rows[1].Number != 5 || rows[0].JSON["q"] != "b" || rows[0].JSON["row_number"] != 3 {
		t.Fatalf("rows: %+v", rows)
	}
	if _, ok := rows[0].JSON["_row_index"]; ok {
		t.Errorf("sheet bookkeeping kept: %v", rows[0].JSON)
	}
}

func TestParseDatasetErrors(t *testing.T) {
	for _, c := range []struct {
		cfg  map[string]interface{}
		want string
	}{
		{map[string]interface{}{}, "no data table"},
		{map[string]interface{}{"source": "googleSheets"}, "no spreadsheet"},
		{map[string]interface{}{"source": "csv"}, "unknown source"},
		{map[string]interface{}{"table": "t", "limit_rows": true, "max_rows": 0.0}, "at least 1"},
		{map[string]interface{}{"table": "t", "filters": `[{"value": 1}]`}, "no column"},
	} {
		if _, err := ParseDataset(c.cfg); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.cfg, err, c.want)
		}
	}
}

// Record merges into a case, the first value of a name winning, and only
// into a case of the run it names.
func TestRecordFirstValueWins(t *testing.T) {
	db := testdb.Open(t).DB
	ctx := context.Background()
	s := NewStore(db, "p")
	run, _ := s.CreateRun(ctx, "wf", nil)
	c := &Case{}
	if err := s.AddCase(ctx, run.ID, c); err != nil {
		t.Fatal(err)
	}
	if err := Record(ctx, db, run.ID, c.ID, KindMetrics, map[string]interface{}{"a": 1.0}); err != nil {
		t.Fatal(err)
	}
	if err := Record(ctx, db, run.ID, c.ID, KindMetrics, map[string]interface{}{"a": 2.0, "b": 3.0}); err != nil {
		t.Fatal(err)
	}
	if err := Record(ctx, db, "other", c.ID, KindMetrics, map[string]interface{}{"c": 1.0}); err == nil {
		t.Error("recorded into a case of another run")
	}
	if err := s.LoadCase(ctx, c); err != nil {
		t.Fatal(err)
	}
	if c.Metrics["a"] != 1.0 || c.Metrics["b"] != 3.0 || len(c.Metrics) != 2 {
		t.Errorf("metrics %v", c.Metrics)
	}
	// Another profile does not see the run.
	if r, err := NewStore(db, "q").GetRun(ctx, run.ID); err != nil || r != nil {
		t.Errorf("other profile: %v %v", r, err)
	}
}

// checkWorkflow refuses what n8n refuses before a test run.
func TestCheckWorkflow(t *testing.T) {
	trig := workflow.WorkflowNode{ID: "t", Name: "Rows", Type: workflow.TriggerTypeEvaluation}
	metric := func(cfg map[string]interface{}) workflow.WorkflowNode {
		cfg["operation"] = "setMetrics"
		return workflow.WorkflowNode{ID: "m", Name: "M", Type: NodeType, Config: cfg}
	}
	disabled := trig
	disabled.Disabled = true
	cases := []struct {
		nodes []workflow.WorkflowNode
		conns []workflow.WorkflowConnection
		code  string
	}{
		{nil, nil, ErrTriggerNotFound},
		{[]workflow.WorkflowNode{disabled}, nil, ErrTriggerDisabled},
		{[]workflow.WorkflowNode{trig}, nil, ErrMetricsNodeNotFound},
		{[]workflow.WorkflowNode{trig, metric(map[string]interface{}{"metric": "customMetrics"})}, nil, ErrMetricsNotConfigured},
		{[]workflow.WorkflowNode{trig, metric(map[string]interface{}{})}, nil, ErrMetricsNotConfigured},
		{[]workflow.WorkflowNode{trig, metric(map[string]interface{}{"metric": "helpfulness"})},
			[]workflow.WorkflowConnection{{SourceNodeID: "lm", SourceHandle: "ai_languageModel", TargetNodeID: "m", TargetHandle: "ai_languageModel"}}, ""},
		{[]workflow.WorkflowNode{trig, metric(map[string]interface{}{"metric": "correctness", "runtime": "claude"}),
			{ID: "o", Name: "O", Type: NodeType, Config: map[string]interface{}{"operation": "setOutputs"}}}, nil, ErrOutputsNotConfigured},
		{[]workflow.WorkflowNode{trig, metric(map[string]interface{}{"metric": "stringSimilarity"})}, nil, ""},
	}
	for i, c := range cases {
		if _, code, msg := checkWorkflow(&workflow.Workflow{Nodes: c.nodes, Connections: c.conns}); code != c.code {
			t.Errorf("case %d: code %q (%s), want %q", i, code, msg, c.code)
		}
	}
}

// fakeEngine runs no workflow: each case's execution ends with the status
// and records the metrics its function gives.
type fakeEngine struct {
	wf     *workflow.Workflow
	status func(row map[string]interface{}) string
	record func(c workflow.EvaluationCase)
	rows   []map[string]interface{}
	execs  map[string]string
}

func (f *fakeEngine) GetWorkflow(context.Context, string) (*workflow.Workflow, error) {
	return f.wf, nil
}
func (f *fakeEngine) StartEvaluationCase(_ context.Context, _, triggerID string, row map[string]interface{}, c workflow.EvaluationCase) (string, error) {
	if triggerID != "t" {
		return "", context.Canceled
	}
	id := "x" + c.CaseID
	f.rows = append(f.rows, row)
	f.execs[id] = f.status(row)
	f.record(c)
	return id, nil
}
func (f *fakeEngine) GetExecution(_ context.Context, id string) (*workflow.WorkflowExecution, error) {
	start := time.Now()
	end := start.Add(20 * time.Millisecond)
	return &workflow.WorkflowExecution{ID: id, Status: f.execs[id], StartedAt: &start, FinishedAt: &end}, nil
}
func (f *fakeEngine) ExecutionInFlight(_ context.Context, id string) (string, bool, error) {
	return f.execs[id], f.execs[id] == "QUEUED" || f.execs[id] == "RUNNING", nil
}
func (f *fakeEngine) CancelExecution(string) {}
func (f *fakeEngine) ResolveCredentials(_ context.Context, _ string, cfg map[string]interface{}) map[string]interface{} {
	return cfg
}

// A run's cases end success, FAILED_TO_EXECUTE_WORKFLOW (the execution
// failed), NO_METRICS_COLLECTED or INVALID_METRICS; the means cover the
// succeeded cases (and the predefined metrics of cases without metrics).
func TestExecuteCaseOutcomes(t *testing.T) {
	old := pollInterval
	pollInterval = time.Millisecond
	t.Cleanup(func() { pollInterval = old })
	db := testdb.Open(t).DB
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO data_tables (id, profile_id, name, columns, created_at, updated_at)
		VALUES ('tb', 'p', 'D', '[{"name":"kind","type":"string"}]', 'x', 'x')`); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"ok", "ok2", "fail", "none", "bad"} {
		if _, err := db.Exec(`INSERT INTO data_table_rows (table_id, data, created_at, updated_at) VALUES ('tb', ?, 'x', 'x')`,
			`{"kind":"`+kind+`"}`); err != nil {
			t.Fatal(err)
		}
	}
	eng := &fakeEngine{execs: map[string]string{},
		wf: &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
			{ID: "t", Type: workflow.TriggerTypeEvaluation, Config: map[string]interface{}{"table": "D"}},
			{ID: "m", Type: NodeType, Config: map[string]interface{}{"operation": "setMetrics", "metric": "categorization"}}}},
		status: func(row map[string]interface{}) string {
			if row["kind"] == "fail" {
				return "FAILED"
			}
			return "SUCCESS"
		}}
	var runID string
	eng.record = func(c workflow.EvaluationCase) {
		runID = c.RunID
		m := map[string]interface{}{"ok": map[string]interface{}{"score": 1.0}, "ok2": map[string]interface{}{"score": 0.5},
			"bad": map[string]interface{}{"score": "high"}}[map[int]string{0: "ok", 1: "ok2", 4: "bad"}[c.RowNumber]]
		if m != nil {
			if err := Record(ctx, db, c.RunID, c.CaseID, KindMetrics, m.(map[string]interface{})); err != nil {
				t.Error(err)
			}
		}
	}
	run, err := Execute(ctx, db, "p", eng, "wf", Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ status, code string }{{CaseSuccess, ""}, {CaseSuccess, ""}, {CaseError, ErrFailedToExecute},
		{CaseError, ErrNoMetricsCollected}, {CaseError, ErrInvalidMetrics}}
	if run.Status != RunCompleted || len(run.Cases) != 5 || run.Failed != 3 || run.ID != runID {
		t.Fatalf("run %+v", run)
	}
	for i, w := range want {
		if c := run.Cases[i]; c.Status != w.status || c.ErrorCode != w.code {
			t.Errorf("case %d: %s/%s (%s), want %s/%s", i, c.Status, c.ErrorCode, c.ErrorMessage, w.status, w.code)
		}
	}
	if run.Metrics["score"] != 0.75 || run.Metrics["executionTime"] != 20 {
		t.Errorf("means %v", run.Metrics)
	}
	if eng.rows[1]["row_id"] != int64(2) || eng.rows[1]["kind"] != "ok2" {
		t.Errorf("row item %v", eng.rows[1])
	}
}

// movedOn is a run that paused for a person and was approved between the
// in-flight check and the read of its record.
type movedOn struct{ *fakeEngine }

func (movedOn) ExecutionInFlight(context.Context, string) (string, bool, error) {
	return "WAITING", false, nil
}

func (movedOn) GetExecution(_ context.Context, id string) (*workflow.WorkflowExecution, error) {
	return &workflow.WorkflowExecution{ID: id, Status: "RUNNING"}, nil
}

// A case is judged on the status the wait decided on: a pause for a person,
// not the RUNNING its record says by the time it is read.
func TestWaitForJudgesTheStatusItDecidedOn(t *testing.T) {
	exec, err := waitFor(context.Background(), movedOn{}, "e1", 5*time.Second)
	if err != nil || exec == nil || exec.Status != "WAITING" {
		t.Fatalf("waitFor = %+v, %v; want the WAITING it decided on", exec, err)
	}
}
