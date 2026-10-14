package n8nimport_test

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/n8nimport"
)

// GitHub's dispatchAndWait imports as the package op that hands the run
// the execution's resume URL and waits for the call, wait limit included.
func TestGithubDispatchAndWaitImports(t *testing.T) {
	wf, rep := convertNode(t, "n8n-nodes-base.github", 1.1, map[string]interface{}{
		"resource": "workflow", "operation": "dispatchAndWait",
		"owner":      map[string]interface{}{"__rl": true, "mode": "list", "value": "octo"},
		"repository": map[string]interface{}{"__rl": true, "mode": "list", "value": "hello"},
		"workflowId": map[string]interface{}{"__rl": true, "mode": "list", "value": "deploy.yml"},
		"ref":        map[string]interface{}{"__rl": true, "mode": "list", "value": "main"},
		"inputs":     `{"env":"prod"}`,
		"options": map[string]interface{}{"limitWaitTime": map[string]interface{}{"values": map[string]interface{}{
			"limitType": "afterTimeInterval", "resumeAmount": 3.0, "resumeUnit": "hours"}}},
	})
	n := node(t, wf, "Trigger")
	if !strings.Contains(n.Type, "github") || n.Config["operation"] != "dispatchAndWait" {
		t.Fatalf("node = %s %v", n.Type, n.Config)
	}
	wantConfig(t, n, map[string]interface{}{"resource": "workflow", "inputs": `{"env":"prod"}`,
		"options": map[string]interface{}{"limitWaitTime": map[string]interface{}{"values": map[string]interface{}{
			"limitType": "afterTimeInterval", "resumeAmount": 3.0, "resumeUnit": "hours"}}}})
	for _, nr := range rep.Nodes {
		if nr.Status == n8nimport.StatusUnmapped {
			t.Fatalf("%s unmapped: %+v", nr.Name, nr.Issues)
		}
	}
}
