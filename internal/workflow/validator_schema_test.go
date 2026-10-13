package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

// vwf builds a workflow from nodes chained in order (n[0] → n[1] → …).
func vwf(nodes ...WorkflowNode) *Workflow {
	w := &Workflow{Name: "v", Nodes: nodes}
	for i := 1; i < len(nodes); i++ {
		w.Connections = append(w.Connections, WorkflowConnection{
			ID: nodes[i-1].ID + "-" + nodes[i].ID, SourceNodeID: nodes[i-1].ID, TargetNodeID: nodes[i].ID,
			SourceHandle: "main", TargetHandle: "main",
		})
	}
	return w
}

func vnode(id, typ, name string, cfg map[string]interface{}) WorkflowNode {
	return WorkflowNode{ID: id, Type: typ, Name: name, Config: cfg}
}

var trig = vnode("t", "trigger.manual", "Start", map[string]interface{}{})

func assigns(field, value string) []interface{} {
	return []interface{}{map[string]interface{}{"field": field, "value": value}}
}

func knownTypes(types ...string) func(string) bool {
	m := map[string]bool{}
	for _, t := range types {
		m[t] = true
	}
	return func(t string) bool { return m[t] }
}

type wantIssue struct{ node, field, code, severity string }

func TestValidateWorkflowIssues(t *testing.T) {
	cases := []struct {
		name  string
		w     *Workflow
		known func(string) bool
		want  []wantIssue
		valid bool
	}{
		{
			name: "repro: typo'd key, bad method, dangling node ref",
			w: vwf(trig,
				vnode("h", "http.request", "Fetch", map[string]interface{}{"urll": "https://x", "method": "FETCH"}),
				vnode("s", "core.set", "Set", map[string]interface{}{"value": `{{ $node["Nope"].json.x }}`})),
			want: []wantIssue{
				{"h", "url", IssueMissingRequired, "error"},
				{"h", "method", IssueInvalidOption, "error"},
				{"h", "urll", IssueUnknownField, "warning"},
				{"s", "assignments", IssueMissingRequired, "error"},
				{"s", "value", IssueUnknownField, "warning"},
				{"s", "value", IssueDanglingNodeRef, "error"},
			},
		},
		{
			name:  "unknown node type",
			w:     vwf(trig, vnode("x", "acme.list_deals", "Deals", nil)),
			known: knownTypes("core.set"),
			want:  []wantIssue{{"x", "", IssueUnknownNodeType, "error"}},
		},
		{
			name:  "no registry: types without a schema are not judged",
			w:     vwf(trig, vnode("x", "acme.list_deals", "Deals", nil)),
			valid: true,
		},
		{
			name:  "required satisfied by an expression",
			w:     vwf(trig, vnode("h", "http.request", "Fetch", map[string]interface{}{"url": "{{ $json.url }}"})),
			valid: true,
		},
		{
			name:  "required with a default is not missing (method)",
			w:     vwf(trig, vnode("h", "http.request", "Fetch", map[string]interface{}{"url": "https://x"})),
			valid: true,
		},
		{
			name:  "select value as expression is skipped; case-insensitive options",
			w:     vwf(trig, vnode("h", "http.request", "Fetch", map[string]interface{}{"url": "https://x", "method": "{{ $json.m }}"}), vnode("h2", "http.request", "Fetch2", map[string]interface{}{"url": "https://x", "method": "post"})),
			valid: true,
		},
		{
			name: "generic engine keys are not unknown",
			w: vwf(trig, vnode("h", "http.request", "Fetch", map[string]interface{}{
				"url": "https://x", "on_error": "continue", "retry_policy": map[string]interface{}{"max_retries": 2}, "notes": "n", "credential_id": "c",
			})),
			valid: true,
		},
		{
			name:  "node ref by name or id resolves",
			w:     vwf(trig, vnode("s", "core.set", "Set", map[string]interface{}{"assignments": assigns("a", `{{ $node["Start"].json.x }}{{ $node["t"].json.y }}`)})),
			valid: true,
		},
		{
			name: "unbalanced braces",
			w:    vwf(trig, vnode("s", "core.set", "Set", map[string]interface{}{"assignments": assigns("a", "{{ $json.x ")})),
			want: []wantIssue{{"s", "assignments[0].value", IssueBadExpression, "error"}},
		},
		{
			name: "closing braces without opening",
			w:    vwf(trig, vnode("s", "core.set", "Set", map[string]interface{}{"assignments": assigns("a", "{ $json.x }}")})),
			want: []wantIssue{{"s", "assignments[0].value", IssueBadExpression, "error"}},
		},
		{
			name: "n8n $('X') syntax",
			w:    vwf(trig, vnode("s", "core.set", "Set", map[string]interface{}{"assignments": assigns("a", "{{ $('Gone').json.x }}")})),
			want: []wantIssue{
				{"s", "assignments[0].value", IssueBadExpression, "error"},
				{"s", "assignments[0].value", IssueDanglingNodeRef, "error"},
			},
		},
		{
			name: "required under depends_on: missing when the condition holds (default operation)",
			w:    vwf(trig, vnode("d", "data.html", "HTML", map[string]interface{}{})),
			want: []wantIssue{{"d", "field", IssueMissingRequired, "error"}},
		},
		{
			name:  "required under depends_on: not required when the condition does not hold",
			w:     vwf(trig, vnode("d", "data.html", "HTML", map[string]interface{}{"operation": "generate", "template": "<p></p>"})),
			valid: true,
		},
		{
			name:  "required under depends_on: controlling value is an expression",
			w:     vwf(trig, vnode("d", "data.html", "HTML", map[string]interface{}{"operation": "{{ $json.op }}"})),
			valid: true,
		},
		{
			name:  "empty account picker is a warning (bound per machine)",
			w:     vwf(trig, vnode("o", "service.outlook_mail", "Mail", map[string]interface{}{"credential_id": ""})),
			want:  []wantIssue{{"o", "credential_id", IssueMissingRequired, "warning"}},
			valid: true,
		},
		{
			name: "structural error keeps its string and gains an issue",
			w:    &Workflow{Name: "v", Nodes: []WorkflowNode{vnode("s", "core.set", "Set", map[string]interface{}{"assignments": assigns("a", "1")})}},
			want: []wantIssue{{"", "", IssueInvalidWorkflow, "error"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := ValidateWorkflow(tc.w, ValidateOptions{KnownNodeType: tc.known})
			var got []wantIssue
			for _, is := range rep.Issues {
				got = append(got, wantIssue{is.NodeID, is.Field, is.Code, is.Severity})
				if is.Message == "" {
					t.Errorf("issue %+v has no message", is)
				}
			}
			if len(got) != len(tc.want) {
				t.Fatalf("issues = %+v, want %+v", rep.Issues, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("issue %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
			wantValid := tc.valid
			if rep.Valid != wantValid {
				t.Errorf("valid = %v, want %v (errors %v)", rep.Valid, wantValid, rep.Errors)
			}
			nErr := 0
			for _, is := range rep.Issues {
				if is.Severity == "error" {
					nErr++
				}
			}
			if len(rep.Errors) != nErr || len(rep.Warnings) != len(rep.Issues)-nErr {
				t.Errorf("errors/warnings strings out of step with issues: %+v", rep)
			}
		})
	}
}

func TestValidateWorkflowHints(t *testing.T) {
	rep := ValidateWorkflow(vwf(trig,
		vnode("h", "http.request", "Fetch", map[string]interface{}{"urll": "https://x"}),
		vnode("x", "acme.list_deals", "D", nil)),
		ValidateOptions{KnownNodeType: knownTypes()})
	hints := map[string]string{}
	for _, is := range rep.Issues {
		hints[is.Code] = is.Hint
	}
	if !strings.Contains(hints[IssueUnknownField], `did you mean "url"`) {
		t.Errorf("unknown_field hint = %q", hints[IssueUnknownField])
	}
	if !strings.Contains(hints[IssueUnknownNodeType], "automation") {
		t.Errorf("unknown_node_type hint = %q", hints[IssueUnknownNodeType])
	}
}

// Validation stays out of save and activation: the repro still saves and
// activates.
func TestValidateWorkflowDoesNotTightenActivation(t *testing.T) {
	w := vwf(trig,
		vnode("h", "http.request", "Fetch", map[string]interface{}{"urll": "https://x", "method": "FETCH"}))
	if err := ValidateForActivation(w); err != nil {
		t.Fatalf("ValidateForActivation = %v, want nil", err)
	}
}

// Every bundled template validates without errors.
func TestBundledTemplatesValidate(t *testing.T) {
	tmpls := ListTemplates()
	if len(tmpls) == 0 {
		t.Fatal("no bundled templates")
	}
	for _, tm := range tmpls {
		wf, ok := GetTemplate(tm.ID)
		if !ok {
			t.Fatalf("template %s missing", tm.ID)
		}
		raw, err := json.Marshal(wf)
		if err != nil {
			t.Fatal(err)
		}
		w, err := ParseWorkflowFileBytes(raw)
		if err != nil {
			t.Fatalf("%s: %v", tm.ID, err)
		}
		rep := ValidateWorkflow(&w, ValidateOptions{})
		if !rep.Valid {
			t.Errorf("template %s: errors %v", tm.ID, rep.Errors)
		}
		for _, is := range rep.Issues {
			t.Logf("template %s: %s %s %s", tm.ID, is.Code, is.NodeID, is.Message)
		}
	}
}
