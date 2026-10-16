package workflow

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/vault"
)

// A sub-workflow is an AI run when its parent is: one an AI started (its
// mark goes on), one the org side started, or one whose definition the user
// never approved. The user's own run starts the child as theirs. The child's
// own approval, not the parent's, decides whether it is unapproved itself
// (#831 round 5, F2).
func TestASubWorkflowInheritsItsParentsAIRun(t *testing.T) {
	_, store := approvalsTestDB(t)
	eng := newTestEngine(t, store, NewNodeTypeRegistry())
	base := vault.ContextWithExecIDs(vault.ContextWithProfileID(context.Background(), "default"), "wf-parent", "exec-parent")
	child := approvalsTestWorkflow("wf-child", 1)
	child.Nodes[0].Type = TriggerTypeExecuteWorkflow
	createSQLWorkflow(t, store, child)

	called := map[string]interface{}{AICallerKey: UnapprovedWorkflowCaller, UnapprovedCallerKey: "wf-grandparent"}
	for name, tc := range map[string]struct {
		ctx        context.Context
		want       string
		unapproved string
	}{
		"the user's run":                 {WithTrigger(base, "trigger.manual", map[string]interface{}{}), "", ""},
		"a chat's run":                   {WithTrigger(base, "trigger.manual", map[string]interface{}{AICallerKey: "chat"}), "chat", ""},
		"an org's call":                  {WithTrigger(base, TriggerTypeOrgTool, map[string]interface{}{}), "org", ""},
		"an unapproved run":              {WithAIAuthored(WithTrigger(base, "trigger.schedule", map[string]interface{}{})), UnapprovedWorkflowCaller, "wf-parent"},
		"a chat's unapproved run":        {WithAIAuthored(WithTrigger(base, "trigger.manual", map[string]interface{}{AICallerKey: "chat"})), "chat", ""},
		"a run an unapproved one called": {WithTrigger(base, TriggerTypeExecuteWorkflow, called), UnapprovedWorkflowCaller, "wf-grandparent"},
	} {
		id, err := eng.StartSubWorkflow(tc.ctx, SubWorkflowStart{ParentExecutionID: "parent", ParentNodeID: "call", Workflow: "wf-child", ItemIndex: -1})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		exec, err := store.GetExecution(base, id)
		if err != nil || exec == nil {
			t.Fatalf("%s: the child's execution: %v", name, err)
		}
		if got, _ := exec.TriggerData[AICallerKey].(string); got != tc.want {
			t.Errorf("%s: the child's AI caller is %q, want %q", name, got, tc.want)
		}
		// The workflow to approve, for the child's refusals (#831 round 6).
		if got, ok := UnapprovedCaller(WithTrigger(base, TriggerTypeExecuteWorkflow, exec.TriggerData)); got != tc.unapproved || ok != (tc.want == UnapprovedWorkflowCaller) {
			t.Errorf("%s: the child's unapproved caller is %q (%v), want %q", name, got, ok, tc.unapproved)
		}
	}
}
