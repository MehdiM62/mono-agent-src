package workflow

import (
	"strings"
	"testing"
)

// publication.register takes approval (the approved send a run an AI
// started names), as a value or an expression per item: `workflow validate`
// knows the field (#831).
func TestPublicationRegisterApprovalValidates(t *testing.T) {
	for _, approval := range []string{"{{ $json.hil_id }}", "7c1e2d4f"} {
		w := &Workflow{ID: "w", Name: "Publish", Nodes: []WorkflowNode{
			{ID: "t", WorkflowID: "w", Type: "trigger.manual", Name: "Start"},
			{ID: "r", WorkflowID: "w", Type: "publication.register", Name: "Register",
				Config: map[string]interface{}{"platform": "x", "kind": "post", "body": "hi", "approval": approval}},
		}, Connections: []WorkflowConnection{{SourceNodeID: "t", SourceHandle: "main", TargetNodeID: "r", TargetHandle: "main"}}}
		rep := ValidateWorkflow(w, ValidateOptions{})
		if !rep.Valid {
			t.Errorf("approval %q: invalid: %v", approval, rep.Errors)
		}
		for _, is := range rep.Issues {
			if is.Field == "approval" && is.Code != IssueUnknownUpstreamField {
				t.Errorf("approval %q: %+v", approval, is)
			}
		}
	}
	s, err := LoadDefaultSchema("publication.register")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range s.Fields {
		if f.Key == "approval" {
			if f.Required || !strings.Contains(f.Help, "approved") {
				t.Errorf("the approval field: %+v", f)
			}
			return
		}
	}
	t.Error("the publication.register schema has no approval field")
}
