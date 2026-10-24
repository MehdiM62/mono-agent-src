package aitask

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// wfResult builds a workflow-generate result around nodes/connections JSON.
func wfResult(t *testing.T, nodes, conns string, extra map[string]any) []byte {
	t.Helper()
	var n, c any
	if err := json.Unmarshal([]byte(nodes), &n); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(conns), &c); err != nil {
		t.Fatal(err)
	}
	wf := map[string]any{"name": "Fetch", "description": "GET a URL", "is_active": false, "nodes": n, "connections": c}
	for k, v := range extra {
		wf[k] = v
	}
	b, err := json.Marshal(map[string]any{
		"summary":    "Fetches a URL.",
		"workflow":   wf,
		"validation": map[string]any{"valid": true, "errors": []string{}, "warnings": []string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const (
	goodNodes = `[{"id":"n1","type":"trigger.manual","name":"Start","position":{"x":100,"y":100},"config":{}},
	  {"id":"n2","type":"http.request","name":"Fetch","position":{"x":350,"y":100},"config":{"method":"GET","url":"https://example.com/api"}}]`
	goodConns = `[{"id":"c1","source":"n1","source_handle":"main","target":"n2","target_handle":"main"}]`
)

func withKnownTypes(t *testing.T, known ...string) {
	t.Helper()
	old := WorkflowNodeTypeKnown
	WorkflowNodeTypeKnown = func(nt string) bool {
		for _, k := range known {
			if k == nt {
				return true
			}
		}
		return false
	}
	t.Cleanup(func() { WorkflowNodeTypeKnown = old })
}

func TestWorkflowKindsAreRegisteredAtTheirAccess(t *testing.T) {
	// workflow-generate reads templates and workflows (workspace); failed-run-fix
	// only diagnoses (qa). Neither may run unattended.
	for name, access := range map[string]string{KindWorkflowGenerate: AccessWorkspace, KindFailedRunFix: AccessQA} {
		k, err := Lookup(name)
		if err != nil {
			t.Fatal(err)
		}
		if k.DefaultAccess != access || k.UnattendedOK || k.Validate == nil || k.Schema == nil {
			t.Errorf("%s: %+v", name, k)
		}
	}
}

func TestWorkflowGenerateResultIsCheckedByTheRealValidator(t *testing.T) {
	withKnownTypes(t, "trigger.manual", "http.request")
	k, _ := Lookup(KindWorkflowGenerate)
	cases := []struct {
		name    string
		result  []byte
		wantErr string // "" accepts
	}{
		{"a valid workflow", wfResult(t, goodNodes, goodConns, nil), ""},
		{"a required config field missing", wfResult(t,
			`[{"id":"n1","type":"trigger.manual","name":"Start","config":{}},{"id":"n2","type":"http.request","name":"Fetch","config":{"method":"GET"}}]`,
			goodConns, nil), "does not pass `workflow validate`"},
		{"an invented node type", wfResult(t,
			`[{"id":"n1","type":"trigger.manual","name":"Start","config":{}},{"id":"n2","type":"http.nope","name":"Fetch","config":{}}]`,
			goodConns, nil), `unknown node type "http.nope"`},
		{"a connection to a node that is not there", wfResult(t, goodNodes,
			`[{"id":"c1","source":"n1","source_handle":"main","target":"n9","target_handle":"main"}]`, nil), "does not pass `workflow validate`"},
		{"no trigger", wfResult(t,
			`[{"id":"n2","type":"http.request","name":"Fetch","config":{"method":"GET","url":"https://example.com"}}]`, `[]`, nil), "does not pass `workflow validate`"},
		{"a workflow that switches itself on", wfResult(t, goodNodes, goodConns, map[string]any{"is_active": true}), "is_active must be false"},
		{"a node with no type", wfResult(t, `[{"id":"n1","name":"x","config":{}}]`, `[]`, nil), `node "n1" has no "type"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var v any
			if err := json.Unmarshal(c.result, &v); err != nil {
				t.Fatal(err)
			}
			if err := Validate(k.Schema, v); err != nil {
				t.Fatalf("the shape should pass the schema: %v", err)
			}
			err := k.Validate(v)
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("refused a valid workflow: %v", err)
			case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
				t.Fatalf("want an error with %q, got %v", c.wantErr, err)
			}
		})
	}
}

func TestWorkflowGenerateSchemaRefusesAMalformedShape(t *testing.T) {
	k, _ := Lookup(KindWorkflowGenerate)
	cases := map[string]string{
		"no validation block":  `{"summary":"s","workflow":{"name":"n","nodes":[{"id":"a"}]}}`,
		"no summary":           `{"workflow":{"name":"n","nodes":[{"id":"a"}]},"validation":{"valid":true}}`,
		"empty nodes":          `{"summary":"s","workflow":{"name":"n","nodes":[]},"validation":{"valid":true}}`,
		"workflow as a string": `{"summary":"s","workflow":"{}","validation":{"valid":true}}`,
	}
	for name, body := range cases {
		var v any
		_ = json.Unmarshal([]byte(body), &v)
		if err := Validate(k.Schema, v); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFailedRunFixResultRules(t *testing.T) {
	k, _ := Lookup(KindFailedRunFix)
	edit := `{"summary":"s","diagnosis":"d","fix":{"kind":"workflow-edit","description":"x","proposed":{"nodeId":"n1","config":{"url":"https://example.com"}}},`
	cases := []struct {
		name, body, wantErr string
	}{
		{"an edit that proposes its change as data", edit[:len(edit)-1] + `}`, ""},
		{"a config fix", `{"summary":"s","diagnosis":"d","fix":{"kind":"config","description":"reconnect the account"}}`, ""},
		{"no fix found", `{"summary":"s","diagnosis":"d","fix":{"kind":"none","description":"the site was down"}}`, ""},
		{"an edit that proposes nothing", `{"summary":"s","diagnosis":"d","fix":{"kind":"workflow-edit","description":"x"}}`, "fix.proposed is empty"},
		{"an edit with an empty proposal", `{"summary":"s","diagnosis":"d","fix":{"kind":"workflow-edit","description":"x","proposed":{}}}`, "fix.proposed is empty"},
		{"a claim of changed workflows", edit + `"changed":["wf-1"]}`, "unexpected property"},
		{"a claim of a re-run", `{"summary":"s","diagnosis":"d","fix":{"kind":"none","description":"x"},"reran":true,"rerunExecutionId":"ex-2"}`, "unexpected property"},
		{"a fix kind that does not exist", `{"summary":"s","diagnosis":"d","fix":{"kind":"rewrite-everything","description":"x"}}`, "must be one of"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var v any
			if err := json.Unmarshal([]byte(c.body), &v); err != nil {
				t.Fatal(err)
			}
			err := Validate(k.Schema, v)
			if err == nil {
				err = k.Validate(v)
			}
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
				t.Fatalf("want %q, got %v", c.wantErr, err)
			}
		})
	}
}

// Through the service: a task of the kind starts at workspace access, its
// prompt carries the method, and Complete refuses an invalid workflow with the
// validator's reasons (leaving the task running) and accepts a valid one.
func TestWorkflowGenerateThroughTheService(t *testing.T) {
	withKnownTypes(t, "trigger.manual", "http.request")
	s, l := newService(t)
	task, err := s.Submit(context.Background(), SubmitInput{Kind: KindWorkflowGenerate, Goal: "fetch a url and tag it"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Access != AccessWorkspace {
		t.Fatalf("access = %q", task.Access)
	}
	for _, want := range []string{"fetch a url and tag it", "monoagentcli node schema", "the same validator", "REPLACE_ME", `"required"`, "Do not save, import, run or activate"} {
		if !strings.Contains(task.Prompt, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	token := l.tokens[0]
	bad := wfResult(t, `[{"id":"n1","type":"trigger.manual","name":"Start","config":{}},{"id":"n2","type":"http.nope","name":"F","config":{}}]`, goodConns, nil)
	_, err = s.Complete(task.ID, token, bad)
	if code(err) != CodeSchemaInvalid || !strings.Contains(err.Error(), `unknown node type "http.nope"`) || !strings.Contains(err.Error(), "run complete again") {
		t.Fatalf("an invalid workflow must be refused with the validator's reason: %v", err)
	}
	if got, _ := s.Store.Get(task.ID); got.Status != StatusRunning {
		t.Fatalf("a refused result changed the task: %s", got.Status)
	}
	done, err := s.Complete(task.ID, token, wfResult(t, goodNodes, goodConns, nil))
	if err != nil || done.Status != StatusComplete {
		t.Fatalf("%+v %v", done, err)
	}
}

// failed-run-fix diagnoses only: its prompt says so, offers no edit command
// and has no field for a change made or a run started.
func TestFailedRunFixPromptDiagnosesOnly(t *testing.T) {
	k, _ := Lookup(KindFailedRunFix)
	for _, want := range []string{"DIAGNOSE only", "edit nothing", "fix.proposed"} {
		if !strings.Contains(k.PromptTemplate, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	for _, bad := range []string{"workflow node set", "workflow edit", "reran", "rerunExecutionId", "changed lists"} {
		if strings.Contains(k.PromptTemplate, bad) {
			t.Errorf("prompt still mentions %q", bad)
		}
	}
}
