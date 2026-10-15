package chat

import (
	"context"
	"strings"
	"testing"
)

// A tool set built without a profile refuses every call: there is no default
// profile to fall back to.
func TestToolsWithoutProfileRefuseEveryCall(t *testing.T) {
	db := newMonoagentTestDB(t)
	if _, err := NewMonoagentTools(db.DB, "", "").ExecuteContext(context.Background(), "list_workflows", "{}"); err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("MonoagentTools without a profile: want a profile error, got %v", err)
	}
	if _, err := NewCanvasTools(db.DB, "").Execute("get_workflow_state", "{}"); err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("CanvasTools without a profile: want a profile error, got %v", err)
	}
}
