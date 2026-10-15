package n8nimport_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/n8nimport"
	"github.com/monoes/mono-agent/internal/workflow"
)

func convertRemoveDuplicates(t *testing.T, params map[string]interface{}) (workflow.WorkflowFileNode, n8nimport.NodeReport, string) {
	t.Helper()
	p, _ := json.Marshal(params)
	src := fmt.Sprintf(`{"name":"d","nodes":[{"name":"Dedupe","type":"n8n-nodes-base.removeDuplicates","typeVersion":2,"position":[0,0],"parameters":%s}],"connections":{}}`, p)
	wf, rep, err := n8nimport.Convert([]byte(src))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	return node(t, wf, "Dedupe"), nodeReport(t, rep, "Dedupe"), issueText(rep, "Dedupe")
}

// Remove Items Processed in Previous Executions keeps its dedupe value,
// scope and history size on core.remove_duplicates' persisted history.
func TestRemoveDuplicatesAcrossRuns(t *testing.T) {
	n, nr, text := convertRemoveDuplicates(t, map[string]interface{}{
		"operation":   "removeItemsSeenInPreviousExecutions",
		"dedupeValue": "={{ $json.id }}",
		"options":     map[string]interface{}{"scope": "workflow", "historySize": 500},
	})
	if n.Type != "core.remove_duplicates" || nr.Status != n8nimport.StatusFull {
		t.Fatalf("type %s, status %s:\n%s", n.Type, nr.Status, text)
	}
	wantConfig(t, n, map[string]interface{}{
		"operation": "removeItemsRepeatedWithinPreviousExecutions", "scope": "workflow", "historySize": float64(500),
	})
	if v, _ := n.Config["dedupeValue"].(string); !strings.Contains(v, "{{") || !strings.Contains(v, "id") {
		t.Errorf("dedupeValue %q", v)
	}
}

// n8n's default scope (node) is left at mono's default.
func TestRemoveDuplicatesAcrossRunsDefaults(t *testing.T) {
	n, _, _ := convertRemoveDuplicates(t, map[string]interface{}{
		"operation": "removeItemsSeenInPreviousExecutions", "dedupeValue": "={{ $json.link }}",
	})
	if _, ok := n.Config["scope"]; ok {
		t.Errorf("scope set: %v", n.Config)
	}
	if _, ok := n.Config["historySize"]; ok {
		t.Errorf("historySize set: %v", n.Config)
	}
}

// A plain-text dedupe value is kept but reported: n8n compares the same
// constant for every item, mono reads a field.
func TestRemoveDuplicatesPlainDedupeValueWarns(t *testing.T) {
	n, nr, text := convertRemoveDuplicates(t, map[string]interface{}{
		"operation": "removeItemsSeenInPreviousExecutions", "dedupeValue": "id",
	})
	if n.Config["dedupeValue"] != "id" || nr.Status != n8nimport.StatusPartial || !strings.Contains(text, "field path") {
		t.Errorf("config %v, status %s:\n%s", n.Config, nr.Status, text)
	}
}

// The high-water-mark logics map to core.remove_duplicates' logic, each
// reading its own n8n value parameter into dedupeValue.
func TestRemoveDuplicatesHighWaterMarkLogics(t *testing.T) {
	for logic, param := range map[string]string{
		"removeItemsUpToStoredIncrementalKey": "incrementalDedupeValue",
		"removeItemsUpToStoredDate":           "dateDedupeValue",
	} {
		n, nr, text := convertRemoveDuplicates(t, map[string]interface{}{
			"operation": "removeItemsSeenInPreviousExecutions", "logic": logic, param: "={{ $json.n }}",
			"dedupeValue": "={{ $json.ignored }}", "options": map[string]interface{}{"scope": "workflow"},
		})
		if n.Type != "core.remove_duplicates" || nr.Status != n8nimport.StatusFull {
			t.Errorf("%s: type %s, status %s:\n%s", logic, n.Type, nr.Status, text)
			continue
		}
		wantConfig(t, n, map[string]interface{}{
			"operation": "removeItemsRepeatedWithinPreviousExecutions", "logic": logic, "scope": "workflow"})
		if v, _ := n.Config["dedupeValue"].(string); !strings.Contains(v, "{{") || !strings.Contains(v, ".n") || strings.Contains(v, "ignored") {
			t.Errorf("%s: dedupeValue %q, want the %s expression", logic, v, param)
		}
	}
}

func TestRemoveDuplicatesUnknownLogicUnsupported(t *testing.T) {
	_, nr, text := convertRemoveDuplicates(t, map[string]interface{}{
		"operation": "removeItemsSeenInPreviousExecutions", "logic": "removeItemsSomehow",
	})
	if nr.Status != n8nimport.StatusUnmapped || !strings.Contains(text, "removeItemsSomehow") {
		t.Errorf("status %s:\n%s", nr.Status, text)
	}
}

func TestRemoveDuplicatesClearHistory(t *testing.T) {
	n, nr, text := convertRemoveDuplicates(t, map[string]interface{}{
		"operation": "clearDeduplicationHistory", "mode": "cleanDatabase", "options": map[string]interface{}{"scope": "workflow"},
	})
	if n.Type != "core.remove_duplicates" || nr.Status != n8nimport.StatusFull {
		t.Fatalf("type %s, status %s:\n%s", n.Type, nr.Status, text)
	}
	wantConfig(t, n, map[string]interface{}{"operation": "clearDeduplicationHistory", "scope": "workflow"})
}
