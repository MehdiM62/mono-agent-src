package workflow

import "testing"

// A caller's input keeps its own fields and loses the ones only the engine
// writes.
func TestStripEngineKeys(t *testing.T) {
	data := map[string]interface{}{
		AICallerKey: "unapproved-workflow", UnapprovedCallerKey: "wf-x", SubWorkflowItemsKey: []interface{}{1},
		SubWorkflowParentKey: map[string]interface{}{}, "keep": 1, "_mine": 2,
	}
	StripEngineKeys(data)
	if len(data) != 2 || data["keep"] != 1 || data["_mine"] != 2 {
		t.Fatalf("data = %v", data)
	}
	StripEngineKeys(nil) // no input
}
