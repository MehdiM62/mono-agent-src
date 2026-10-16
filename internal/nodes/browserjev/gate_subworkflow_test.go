package browserjev

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/orgsign"
	"github.com/monoes/mono-agent/internal/workflow"
)

// A workflow called by one the user never approved carries that mark
// (workflow.UnapprovedWorkflowCaller) and is held as the unapproved workflow
// is: the stricter of chat's and other's levels, not other's alone.
func TestASubWorkflowOfAnUnapprovedOneIsHeldLikeIt(t *testing.T) {
	for _, k := range append(orgsign.AgentContextMarkers(), browserctl.EnvAICaller, workflow.EnvWorkflowRun) {
		t.Setenv(k, "")
	}
	ctx := workflow.WithTrigger(context.Background(), workflow.TriggerTypeExecuteWorkflow,
		map[string]interface{}{workflow.AICallerKey: workflow.UnapprovedWorkflowCaller})
	c, ok := aiCaller(ctx)
	if !ok || !c.authored || c.kind != browserctl.KindOther || c.by.AgentID != workflow.UnapprovedWorkflowCaller {
		t.Fatalf("aiCaller = %+v, %v; want the unapproved-workflow caller held at the stricter level", c, ok)
	}
}
