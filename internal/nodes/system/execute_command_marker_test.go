package system

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// Every process the node starts carries the run's marker, last, so the node's
// own env can't clear it: what a workflow runs is never the operator.
func TestExecuteCommandMarksTheWorkflowRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	t.Setenv(workflow.EnvWorkflowRun, "")
	for id, want := range map[string]string{"exec-7": "exec-7", "": "node-run"} {
		out, err := (&ExecuteCommandNode{}).Execute(context.Background(), workflow.NodeInput{ExecutionID: id}, map[string]interface{}{
			"command": "sh", "args": []interface{}{"-c", "echo \"[$" + workflow.EnvWorkflowRun + "]\""},
			"env": map[string]interface{}{workflow.EnvWorkflowRun: ""},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(mainJSON(t, out)["stdout"].(string)); got != "["+want+"]" {
			t.Errorf("execution %q: the child saw %s, want [%s]", id, got, want)
		}
	}
}
