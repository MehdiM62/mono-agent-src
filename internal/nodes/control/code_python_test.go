package control

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

func needPython(t *testing.T) {
	t.Helper()
	if _, err := findPython(); err != nil {
		t.Skip("python3 not installed")
	}
}

func pyItems(vals ...float64) []workflow.Item {
	items := make([]workflow.Item, len(vals))
	for i, v := range vals {
		items[i] = workflow.Item{JSON: map[string]interface{}{"n": v}}
	}
	return items
}

func runPy(t *testing.T, in workflow.NodeInput, code string, extra map[string]interface{}) ([]workflow.Item, error) {
	t.Helper()
	cfg := map[string]interface{}{"language": "python", "code": code}
	for k, v := range extra {
		cfg[k] = v
	}
	out, err := (&CodeNode{}).Execute(context.Background(), in, cfg)
	if err != nil {
		return nil, err
	}
	return out[0].Items, nil
}

func TestCodePython_AllItemsShapes(t *testing.T) {
	needPython(t)
	in := workflow.NodeInput{Items: pyItems(1, 2)}
	for name, code := range map[string]string{
		// n8n 2.x task runner (pythonNative) default code.
		"native": "for item in _items:\n  item[\"json\"][\"d\"] = item[\"json\"][\"n\"] * 2\nreturn _items",
		// The legacy Pyodide default: attribute access on _input.all().
		"legacy":     "for item in _input.all():\n  item.json.d = item.json.n * 2\nreturn _input.all()",
		"plain dict": "return [{\"n\": i[\"json\"][\"n\"], \"d\": i[\"json\"][\"n\"] * 2} for i in _items]",
		"to_py":      "return [dict(i.json.to_py(), d=i.json.n * 2) for i in _input.all()]",
	} {
		items, err := runPy(t, in, code, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(items) != 2 || items[1].JSON["d"] != 4.0 || items[1].JSON["n"] != 2.0 {
			t.Errorf("%s: items = %v", name, items)
		}
	}
}

func TestCodePython_SingleDictNoneAndJSON(t *testing.T) {
	needPython(t)
	in := workflow.NodeInput{Items: pyItems(5, 7)}
	items, err := runPy(t, in, "return {\"first\": _json[\"n\"], \"last\": _input.last().json.n, \"count\": len(_items)}", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].JSON["first"] != 5.0 || items[0].JSON["last"] != 7.0 || items[0].JSON["count"] != 2.0 {
		t.Errorf("items = %v", items)
	}
	items, err = runPy(t, in, "x = 1", nil)
	if err != nil || len(items) != 0 {
		t.Errorf("no return: %v, %v", items, err)
	}
}

func TestCodePython_PerItem(t *testing.T) {
	needPython(t)
	in := workflow.NodeInput{Items: pyItems(1, 2, 3)}
	code := "if _item[\"json\"][\"n\"] == 2:\n  return None\n_item[\"json\"][\"sq\"] = _json[\"n\"] ** 2\nreturn _item"
	items, err := runPy(t, in, code, map[string]interface{}{"mode": "runOnceForEachItem"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].JSON["sq"] != 1.0 || items[1].JSON["sq"] != 9.0 {
		t.Errorf("items = %v", items)
	}
	// _input.item is the current item, as in n8n's legacy mode.
	items, err = runPy(t, in, "return {\"v\": _input.item.json.n}", map[string]interface{}{"mode": "runOnceForEachItem"})
	if err != nil || len(items) != 3 || items[2].JSON["v"] != 3.0 {
		t.Errorf("_input.item: %v, %v", items, err)
	}
}

func TestCodePython_NodeAccessorPrintAndContext(t *testing.T) {
	needPython(t)
	in := workflow.NodeInput{
		Items:       pyItems(1),
		NodeOutputs: map[string][]workflow.Item{"Fetch Rows": pyItems(10, 20), "Other": pyItems(3)},
		WorkflowID:  "wf-1",
		RunIndex:    2,
	}
	code := "print('to stderr, not the result')\nreturn [{\"sum\": sum(i.json.n for i in _('Fetch Rows').all()), \"wf\": _workflow[\"id\"], \"run\": _runIndex}]"
	items, err := runPy(t, in, code, nil)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].JSON["sum"] != 30.0 || items[0].JSON["wf"] != "wf-1" || items[0].JSON["run"] != 2.0 {
		t.Errorf("items = %v", items)
	}
	if _, err := runPy(t, in, "return _('Missing').all()", nil); err == nil || !strings.Contains(err.Error(), "Missing") {
		t.Errorf("missing node: %v", err)
	}
}

func TestCodePython_ErrorsCarryTraceback(t *testing.T) {
	needPython(t)
	in := workflow.NodeInput{Items: pyItems(1)}
	_, err := runPy(t, in, "x = 1\ny = x / 0\nreturn []", nil)
	if err == nil || !strings.Contains(err.Error(), "ZeroDivisionError") || !strings.Contains(err.Error(), "line 2: y = x / 0") {
		t.Errorf("runtime error: %v", err)
	}
	_, err = runPy(t, in, "return [", nil)
	if err == nil || !strings.Contains(err.Error(), "SyntaxError") {
		t.Errorf("syntax error: %v", err)
	}
	_, err = runPy(t, in, "return [1, 2]", nil)
	if err == nil || !strings.Contains(err.Error(), "list of dictionaries") {
		t.Errorf("bad shape: %v", err)
	}
}

func TestCodePython_TimeoutAndCancel(t *testing.T) {
	needPython(t)
	in := workflow.NodeInput{Items: pyItems(1)}
	start := time.Now()
	_, err := runPy(t, in, "while True:\n  pass", map[string]interface{}{"timeout_seconds": 1})
	if err == nil || !strings.Contains(err.Error(), "timeout") || time.Since(start) > 10*time.Second {
		t.Errorf("timeout: %v after %v", err, time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	_, err = (&CodeNode{}).Execute(ctx, in, map[string]interface{}{"language": "python", "code": "import time\ntime.sleep(30)"})
	if !errors.Is(err, workflow.ErrExecutionCancelled) {
		t.Errorf("cancel: %v", err)
	}
}

func TestCodePython_MissingInterpreter(t *testing.T) {
	t.Setenv(pythonEnvVar, "/nonexistent/python3")
	_, err := runPy(t, workflow.NodeInput{}, "return []", nil)
	if err == nil {
		t.Fatal("expected an error without a Python interpreter")
	}
	t.Setenv(pythonEnvVar, "")
	t.Setenv("PATH", t.TempDir())
	if _, err := findPython(); err == nil || !strings.Contains(err.Error(), "python3 not found (set MONOAGENT_PYTHON)") {
		t.Errorf("missing python3: %v", err)
	}
}

// The Python child carries the run's marker: what a workflow runs is never
// the operator.
func TestPythonMarksTheWorkflowRun(t *testing.T) {
	needPython(t)
	t.Setenv(workflow.EnvWorkflowRun, "")
	items, err := runPy(t, workflow.NodeInput{ExecutionID: "exec-7", Items: pyItems(1)},
		"import os\nreturn [{'json': {'run': os.environ.get('"+workflow.EnvWorkflowRun+"')}}]", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].JSON["run"] != "exec-7" {
		t.Errorf("items = %v", items)
	}
}
