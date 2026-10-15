package n8nimport_test

import (
	"encoding/json"
	"testing"

	"github.com/monoes/mono-agent/internal/n8nimport"
	"github.com/monoes/mono-agent/internal/noderegistry"
	"github.com/monoes/mono-agent/internal/workflow"
)

const evaluationWorkflow = `{"name":"eval","nodes":[
 {"name":"When fetching a dataset row","type":"n8n-nodes-base.evaluationTrigger","typeVersion":4.7,"position":[0,0],"parameters":{
  "dataTableId":{"__rl":true,"mode":"list","value":"t9","cachedResultName":"Cases"},
  "limitRows":true,"maxRows":3,"filterRows":true,"matchType":"allConditions",
  "filters":{"conditions":[{"keyName":"split","condition":"eq","keyValue":"test"}]}}},
 {"name":"Evaluating?","type":"n8n-nodes-base.evaluation","typeVersion":4.8,"position":[200,0],"parameters":{"operation":"checkIfEvaluating"}},
 {"name":"Inputs","type":"n8n-nodes-base.evaluation","typeVersion":4.8,"position":[400,0],"parameters":{
  "operation":"setInputs","inputs":{"values":[{"inputName":"question","inputValue":"={{ $json.question }}"}]}}},
 {"name":"Outputs","type":"n8n-nodes-base.evaluation","typeVersion":4.8,"position":[600,0],"parameters":{
  "dataTableId":{"__rl":true,"mode":"name","value":"Cases"},
  "outputs":{"values":[{"outputName":"answer","outputValue":"={{ $json.output }}"}]}}},
 {"name":"Similarity","type":"n8n-nodes-base.evaluation","typeVersion":4.8,"position":[800,0],"parameters":{
  "operation":"setMetrics","metric":"stringSimilarity",
  "expectedAnswer":"={{ $('When fetching a dataset row').item.json.expected }}","actualAnswer":"={{ $json.output }}",
  "options":{"metricName":"close"}}},
 {"name":"Correct","type":"n8n-nodes-base.evaluation","typeVersion":4.8,"position":[1000,0],"parameters":{
  "operation":"setMetrics","expectedAnswer":"={{ $json.expected }}","actualAnswer":"={{ $json.output }}"}},
 {"name":"Judge","type":"@n8n/n8n-nodes-langchain.lmChatOpenAi","typeVersion":1.2,"position":[1000,200],"parameters":{
  "model":{"__rl":true,"mode":"list","value":"gpt-4o-mini"}}},
 {"name":"Custom","type":"n8n-nodes-base.evaluation","typeVersion":4.6,"position":[1200,0],"parameters":{
  "operation":"setMetrics","metrics":{"assignments":[{"id":"a","name":"length","value":"={{ $json.output.length }}","type":"number"}]}}}
],"connections":{
 "When fetching a dataset row":{"main":[[{"node":"Evaluating?","type":"main","index":0}]]},
 "Evaluating?":{"main":[[{"node":"Inputs","type":"main","index":0}],[]]},
 "Inputs":{"main":[[{"node":"Outputs","type":"main","index":0}]]},
 "Outputs":{"main":[[{"node":"Similarity","type":"main","index":0}]]},
 "Similarity":{"main":[[{"node":"Correct","type":"main","index":0}]]},
 "Correct":{"main":[[{"node":"Custom","type":"main","index":0}]]},
 "Judge":{"ai_languageModel":[[{"node":"Correct","type":"ai_languageModel","index":0}]]}
}}`

// An n8n evaluation workflow imports node for node: the dataset trigger as
// trigger.evaluation (its table, limit and filters), each Evaluation node
// as core.evaluation with its operation, and checkIfEvaluating's first
// output as the "evaluation" handle. The result passes `workflow validate`.
func TestEvaluationWorkflowImports(t *testing.T) {
	wf, rep, err := n8nimport.Convert([]byte(evaluationWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"When fetching a dataset row", "Evaluating?", "Inputs", "Outputs", "Similarity", "Custom"} {
		if nr := nodeReport(t, rep, name); nr.Status != n8nimport.StatusFull {
			t.Errorf("%s: status %s\n%s", name, nr.Status, issueText(rep, name))
		}
	}
	trig := node(t, wf, "When fetching a dataset row")
	if trig.Type != "trigger.evaluation" {
		t.Fatalf("trigger → %s", trig.Type)
	}
	wantConfig(t, trig, map[string]interface{}{"source": "dataTable", "table": "Cases", "limit_rows": true, "max_rows": 3.0,
		"match": "all", "filters": []interface{}{map[string]interface{}{"column": "split", "condition": "eq", "value": "test"}}})
	wantConfig(t, node(t, wf, "Evaluating?"), map[string]interface{}{"operation": "checkIfEvaluating"})
	wantConfig(t, node(t, wf, "Inputs"), map[string]interface{}{"operation": "setInputs",
		"inputs": []interface{}{map[string]interface{}{"name": "question", "value": "{{ $json.question }}"}}})
	// setOutputs is the default operation; from 4.8 the source is a data table.
	wantConfig(t, node(t, wf, "Outputs"), map[string]interface{}{"operation": "setOutputs", "source": "dataTable", "table": "Cases",
		"outputs": []interface{}{map[string]interface{}{"name": "answer", "value": "{{ $json.output }}"}}})
	wantConfig(t, node(t, wf, "Similarity"), map[string]interface{}{"operation": "setMetrics", "metric": "stringSimilarity",
		"metric_name": "close", "actual_answer": "{{ $json.output }}"})
	// correctness is the default metric; the attached model picks the runtime.
	correct := node(t, wf, "Correct")
	wantConfig(t, correct, map[string]interface{}{"metric": "correctness", "expected_answer": "{{ $json.expected }}"})
	if correct.Config["runtime"] == nil || correct.Config["runtime"] == "" {
		t.Errorf("correctness has no runtime: %v", correct.Config)
	}
	// Before 4.7 every metrics node returns custom metrics.
	wantConfig(t, node(t, wf, "Custom"), map[string]interface{}{"metric": "customMetrics",
		"metrics": []interface{}{map[string]interface{}{"name": "length", "value": "{{ len $json.output }}"}}})

	handle := ""
	for _, c := range wf.Connections {
		if c.Source == node(t, wf, "Evaluating?").ID {
			handle = c.SourceHandle
		}
	}
	if handle != "evaluation" {
		t.Errorf("checkIfEvaluating output 0 → handle %q", handle)
	}

	raw, err := json.Marshal(wf)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := workflow.ParseWorkflowFileBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if v := workflow.ValidateWorkflow(&parsed, workflow.ValidateOptions{KnownNodeType: noderegistry.Build(nil).Has}); !v.Valid || len(v.Issues) > 0 {
		t.Errorf("valid=%v errors=%v issues=%+v", v.Valid, v.Errors, v.Issues)
	}
}

// Google Sheets datasets keep their sheet: the 4.6 trigger (sheets by
// default) reads it with its filters, and setOutputs before 4.8 writes there.
func TestEvaluationSheetsSource(t *testing.T) {
	wf, rep := convertNode(t, "n8n-nodes-base.evaluationTrigger", 4.6, map[string]interface{}{
		"documentId": map[string]interface{}{"__rl": true, "mode": "id", "value": "sheet-1"},
		"sheetName":  map[string]interface{}{"__rl": true, "mode": "name", "value": "Cases"},
		"filtersUI":  map[string]interface{}{"values": []interface{}{map[string]interface{}{"lookupColumn": "split", "lookupValue": "test"}}},
	})
	trig := node(t, wf, "Trigger")
	if trig.Type != "trigger.evaluation" {
		t.Fatalf("trigger → %s\n%s", trig.Type, issueText(rep, "Trigger"))
	}
	wantConfig(t, trig, map[string]interface{}{"source": "googleSheets", "spreadsheet_id": "sheet-1", "sheet_name": "Cases",
		"match": "all", "filters": []interface{}{map[string]interface{}{"column": "split", "condition": "eq", "value": "test"}}})

	wf, _ = convertNode(t, "n8n-nodes-base.evaluation", 4.7, map[string]interface{}{
		"operation":  "setOutputs",
		"documentId": map[string]interface{}{"__rl": true, "mode": "id", "value": "sheet-1"},
		"sheetName":  map[string]interface{}{"__rl": true, "mode": "name", "value": "Cases"},
		"outputs":    map[string]interface{}{"values": []interface{}{map[string]interface{}{"outputName": "answer", "outputValue": "x"}}},
	})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"source": "googleSheets", "spreadsheet_id": "sheet-1", "sheet_name": "Cases"})
}

// Values that are not Evaluation operations import as placeholders that
// say so.
func TestEvaluationUnknownOperation(t *testing.T) {
	_, rep := convertNode(t, "n8n-nodes-base.evaluation", 4.8, map[string]interface{}{"resource": "sheet", "operation": "append"})
	if nr := nodeReport(t, rep, "Trigger"); nr.Status != n8nimport.StatusUnmapped {
		t.Errorf("status %s", nr.Status)
	}
	wantIssue(t, rep, "not an Evaluation operation")
}
