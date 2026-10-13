package mcp

import (
	"encoding/json"
	"testing"
)

// workflow_validate returns the schema-aware issues next to errors[].
func TestServerWorkflowValidateIssues(t *testing.T) {
	s := newTestServer(t)
	inline := map[string]interface{}{
		"name": "repro",
		"nodes": []map[string]interface{}{
			{"id": "t", "type": "trigger.manual", "name": "Start", "config": map[string]interface{}{}},
			{"id": "h", "type": "http.request", "name": "Fetch", "config": map[string]interface{}{"urll": "https://x", "method": "FETCH"}},
			{"id": "x", "type": "acme.list_deals", "name": "Deals", "config": map[string]interface{}{}},
		},
		"connections": []map[string]interface{}{
			{"source": "t", "source_handle": "main", "target": "h", "target_handle": "main"},
			{"source": "h", "source_handle": "main", "target": "x", "target_handle": "main"},
		},
	}
	resps := serveLines(t, s, callToolReq(1, "workflow_validate", map[string]interface{}{"workflow": inline}))
	text, isErr := toolText(t, resps[0])
	if isErr {
		t.Fatalf("unexpected tool error: %s", text)
	}
	var res struct {
		Valid    bool     `json:"valid"`
		Errors   []string `json:"errors"`
		Warnings []string `json:"warnings"`
		Issues   []struct {
			NodeID string `json:"node_id"`
			Field  string `json:"field"`
			Code   string `json:"code"`
			Hint   string `json:"hint"`
		} `json:"issues"`
	}
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, text)
	}
	got := map[string]bool{}
	for _, is := range res.Issues {
		got[is.Code+"/"+is.NodeID+"/"+is.Field] = true
	}
	for _, want := range []string{"missing_required/h/url", "invalid_option/h/method", "unknown_field/h/urll", "unknown_node_type/x/"} {
		if !got[want] {
			t.Errorf("missing issue %s:\n%s", want, text)
		}
	}
	if res.Valid || len(res.Errors) != 3 || len(res.Warnings) != 1 {
		t.Errorf("want invalid with 3 errors and 1 warning:\n%s", text)
	}
}
