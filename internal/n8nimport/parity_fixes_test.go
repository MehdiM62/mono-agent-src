package n8nimport_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/n8nimport"
	"github.com/monoes/mono-agent/internal/workflow"
)

func typesOf(wf *workflow.WorkflowFile) []string {
	var out []string
	for _, n := range wf.Nodes {
		out = append(out, n.Type)
	}
	return out
}

// An API package operation n8n has but the package lacks is a placeholder
// with a clear message, not an app node that fails at run time.
func TestAppOperationMissingFromPackageIsPlaceholder(t *testing.T) {
	_, rep := convertNode(t, "n8n-nodes-base.googleAnalytics", 2, map[string]interface{}{"resource": "userActivity", "operation": "search"})
	if nr := nodeReport(t, rep, "Trigger"); nr.Status != n8nimport.StatusUnmapped {
		t.Fatalf("status %s", nr.Status)
	}
	wantIssue(t, rep, `operation "userActivity / search" is not in API package google_analytics yet`)
	wf, rep := convertNode(t, "n8n-nodes-base.googleAnalytics", 2, map[string]interface{}{"resource": "report", "operation": "get"})
	if n := node(t, wf, "Trigger"); n.Type != "app.google_analytics" || nodeReport(t, rep, "Trigger").Status == n8nimport.StatusUnmapped {
		t.Errorf("report.get: %s %s", n.Type, rep.Text())
	}
}

func TestExecuteWorkflowImport(t *testing.T) {
	wf, rep := convertNode(t, "n8n-nodes-base.executeWorkflow", 1.2, map[string]interface{}{
		"workflowId": map[string]interface{}{"__rl": true, "mode": "list", "value": "55", "cachedResultName": "Enrich lead"},
		"mode":       "each", "options": map[string]interface{}{"waitForSubWorkflow": false},
		"workflowInputs": map[string]interface{}{"mappingMode": "defineBelow",
			"value":  map[string]interface{}{"email": "={{ $json.mail }}", "score": "={{ $json.s }}"},
			"schema": []interface{}{map[string]interface{}{"id": "email", "type": "string"}, map[string]interface{}{"id": "score", "type": "number"}}},
	})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"workflowId": "Enrich lead", "mode": "each", "waitForSubWorkflow": false})
	set := node(t, wf, "Trigger (inputs)")
	wantConfig(t, set, map[string]interface{}{"include_input": false, "assignments": []interface{}{
		map[string]interface{}{"field": "email", "value": "{{ $json.mail }}", "type": "string"},
		map[string]interface{}{"field": "score", "value": "{{ $json.s }}", "type": "number"}}})
	if nr := nodeReport(t, rep, "Trigger"); nr.Status != n8nimport.StatusFull {
		t.Errorf("by cached name: %s\n%s", nr.Status, rep.Text())
	}
	_, rep = convertNode(t, "n8n-nodes-base.executeWorkflow", 1, map[string]interface{}{"workflowId": "55"})
	wantIssue(t, rep, "is an n8n workflow ID")
	_, rep = convertNode(t, "n8n-nodes-base.executeWorkflow", 1, map[string]interface{}{"source": "url", "workflowUrl": "https://x"})
	if nodeReport(t, rep, "Trigger").Status != n8nimport.StatusUnmapped {
		t.Errorf("url source should be a placeholder: %s", rep.Text())
	}
}

func TestItemListsRemoveDuplicates(t *testing.T) {
	wf, rep := convertNode(t, "n8n-nodes-base.itemLists", 3.1, map[string]interface{}{
		"operation": "removeDuplicates", "compare": "selectedFields", "fieldsToCompare": "email"})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"field": "email", "keep": "first"})
	if n := node(t, wf, "Trigger"); n.Type != "core.remove_duplicates" {
		t.Errorf("type %s: %s", n.Type, rep.Text())
	}
}

func TestRedisAndMongoUseTheDBTables(t *testing.T) {
	wf, _ := convertNode(t, "n8n-nodes-base.redis", 1, map[string]interface{}{"operation": "llen", "list": "queue"})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"operation": "llen", "list": "queue", "n8n_compat": true})
	wf, rep := convertNode(t, "n8n-nodes-base.mongoDb", 1.2, map[string]interface{}{"resource": "searchIndexes",
		"operation": "createSearchIndex", "collection": "c", "indexNameRequired": "idx", "indexDefinition": "{}"})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"operation": "createSearchIndex", "index_name": "idx", "collection": "c"})
	if nr := nodeReport(t, rep, "Trigger"); nr.Status != n8nimport.StatusFull {
		t.Errorf("mongo search index: %s\n%s", nr.Status, rep.Text())
	}
}

func TestLegacyFileNodes(t *testing.T) {
	wf, _ := convertNode(t, "n8n-nodes-base.readBinaryFile", 1, map[string]interface{}{"filePath": "/tmp/a.txt", "dataPropertyName": "file"})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"operation": "read", "file_selector": "/tmp/a.txt", "data_property_name": "file"})
	wf, _ = convertNode(t, "n8n-nodes-base.readPDF", 1, map[string]interface{}{"encrypted": true, "password": "pw"})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"operation": "pdf", "binary_property_name": "data", "keep_source": "binary", "password": "pw"})
	wf, rep := convertNode(t, "n8n-nodes-base.moveBinaryData", 1.1, map[string]interface{}{"mode": "binaryToJson", "setAllData": false,
		"sourceKey": "file", "destinationKey": "text", "options": map[string]interface{}{"keepSource": true}})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"operation": "text", "binary_property_name": "file",
		"destination_key": "text", "keep_source": "both"})
	if nr := nodeReport(t, rep, "Trigger"); nr.Status != n8nimport.StatusFull {
		t.Errorf("binaryToJson: %s\n%s", nr.Status, rep.Text())
	}
	wf, _ = convertNode(t, "n8n-nodes-base.moveBinaryData", 1.1, map[string]interface{}{"mode": "jsonToBinary",
		"destinationKey": "out", "options": map[string]interface{}{"fileName": "a.json"}})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"operation": "json", "mode": "each", "binary_property_name": "out", "file_name": "a.json"})
}

func TestAITransformRunsTheGeneratedCode(t *testing.T) {
	wf, rep := convertNode(t, "n8n-nodes-base.aiTransform", 1, map[string]interface{}{
		"instructions": "uppercase the name", "jsCode": "return $input.all().map(i => ({json: {n: i.json.name.toUpperCase()}}));"})
	got := runNode(t, wf, "Trigger", []map[string]interface{}{{"name": "ada"}})
	if len(got) != 1 || got[0]["n"] != "ADA" {
		t.Errorf("aiTransform output %v\n%s", got, rep.Text())
	}
	_, rep = convertNode(t, "n8n-nodes-base.aiTransform", 1, map[string]interface{}{"instructions": "x"})
	wantIssue(t, rep, "n8n saved no generated code")
}

func TestAITaskChains(t *testing.T) {
	wf, _ := convertNode(t, lc+"textClassifier", 1.1, map[string]interface{}{"inputText": "={{ $json.body }}",
		"categories": map[string]interface{}{"categories": []interface{}{
			map[string]interface{}{"category": "Sales", "description": "buying"}, map[string]interface{}{"category": "Support"}}},
		"options": map[string]interface{}{"fallback": "other"}})
	ask := node(t, wf, "Trigger (ask)")
	if ask.Type != "agent.ask" || !strings.Contains(ask.Config["prompt"].(string), "- Sales: buying\n- Support\n- Other") ||
		!strings.HasSuffix(ask.Config["prompt"].(string), "{{ $json.body }}") {
		t.Errorf("classifier ask: %v", ask.Config)
	}
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"field": "{{ $json.__n8n_class.category }}",
		"cases": []interface{}{"Sales", "Support", "Other"}})

	wf, _ = convertNode(t, lc+"sentimentAnalysis", 1, map[string]interface{}{"inputText": "hi",
		"options": map[string]interface{}{"categories": "Good, Bad", "includeDetailedResults": true}})
	ask = node(t, wf, "Trigger (ask)")
	schema := ask.Config["output_schema"].(map[string]interface{})["properties"].(map[string]interface{})
	if ask.Config["output_key"] != "sentimentAnalysis" || schema["confidence"] == nil || !equalJSON(schema["category"],
		map[string]interface{}{"type": "string", "enum": []interface{}{"Good", "Bad"}}) {
		t.Errorf("sentiment ask: %v", ask.Config)
	}

	wf, _ = convertNode(t, lc+"informationExtractor", 1.1, map[string]interface{}{"text": "={{ $json.t }}", "schemaType": "fromJson",
		"jsonSchemaExample": `{"name": "Ada", "age": 36, "tags": ["x"]}`})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"output_key": "output", "output_schema": map[string]interface{}{
		"type": "object", "required": []interface{}{"age", "name", "tags"}, "properties": map[string]interface{}{
			"age": map[string]interface{}{"type": "number"}, "name": map[string]interface{}{"type": "string"},
			"tags": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}}}})
}

func TestMCPClientAndAnthropic(t *testing.T) {
	wf, rep := convertNode(t, lc+"mcpClient", 1, map[string]interface{}{"endpointUrl": "https://mcp.test/mcp",
		"tool":       map[string]interface{}{"__rl": true, "mode": "list", "value": "get_weather"},
		"parameters": map[string]interface{}{"value": map[string]interface{}{"city": "={{ $json.city }}"}},
		"options":    map[string]interface{}{"timeout": 30000}})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"transport": "http", "url": "https://mcp.test/mcp",
		"operation": "call_tool", "tool": "get_weather", "arguments": `{"city":"{{ $json.city }}"}`, "timeout": 30})
	if n := node(t, wf, "Trigger"); n.Type != "mcp.call" {
		t.Errorf("mcp type %s: %s", n.Type, rep.Text())
	}
	wf, _ = convertNode(t, lc+"anthropic", 1, map[string]interface{}{"resource": "text", "operation": "message",
		"modelId":  map[string]interface{}{"__rl": true, "mode": "list", "value": "claude-sonnet-4-5"},
		"messages": map[string]interface{}{"values": []interface{}{map[string]interface{}{"content": "={{ $json.q }}"}}},
		"options":  map[string]interface{}{"system": "be brief"}})
	wantConfig(t, node(t, wf, "Trigger (ask)"), map[string]interface{}{"runtime": "claude", "model": "claude-sonnet-4-5",
		"prompt": "{{ $json.q }}", "system_prompt": "be brief"})
	shaped := runNode(t, wf, "Trigger", []map[string]interface{}{{"__n8n_answer": "hi"}})
	if !equalJSON(shaped, []map[string]interface{}{{"content": []interface{}{map[string]interface{}{"type": "text", "text": "hi"}}}}) {
		t.Errorf("anthropic shape: %v", shaped)
	}
}

// The Git node runs the git CLI with one argv entry per value.
func TestGitImportRunsGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "a b.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "--", "a b.txt")
	git("commit", "-q", "-m", "first; echo pwned")

	wf, _ := convertNode(t, "n8n-nodes-base.git", 1.1, map[string]interface{}{"operation": "log", "repositoryPath": repo, "limit": 1})
	wantConfig(t, node(t, wf, "Trigger (git)"), map[string]interface{}{"command": "git", "args": []interface{}{"log", "-n", "1"}, "working_dir": repo})
	out := runNode(t, wf, "Trigger (git)", []map[string]interface{}{{}})
	if s, _ := out[0]["stdout"].(string); !strings.Contains(s, "first; echo pwned") {
		t.Errorf("log: %v", out)
	}

	wf, _ = convertNode(t, "n8n-nodes-base.git", 1.1, map[string]interface{}{"operation": "switchBranch", "repositoryPath": repo,
		"branchName": "feat/x", "options": map[string]interface{}{"setUpstream": true}})
	if out := runNode(t, wf, "Trigger (git)", []map[string]interface{}{{}}); out[0]["exit_code"] != 0.0 && out[0]["exit_code"] != 0 {
		t.Fatalf("switchBranch: %v", out)
	}
	if b := git("rev-parse", "--abbrev-ref", "HEAD"); b != "feat/x" {
		t.Errorf("branch = %s", b)
	}
	if r := git("config", "branch.feat/x.remote"); r != "origin" {
		t.Errorf("upstream remote = %s", r)
	}
	wf, _ = convertNode(t, "n8n-nodes-base.git", 1.1, map[string]interface{}{"operation": "switchBranch", "repositoryPath": repo, "branchName": "--orphan"})
	if out := runNode(t, wf, "Trigger (git)", []map[string]interface{}{{}}); out[0]["exit_code"] == 0.0 || out[0]["exit_code"] == 0 {
		t.Errorf("a branch name starting with - must be refused: %v", out)
	}

	wf, _ = convertNode(t, "n8n-nodes-base.git", 1.1, map[string]interface{}{"operation": "add", "repositoryPath": repo, "pathsToAdd": "a b.txt, c.txt"})
	wantConfig(t, node(t, wf, "Trigger (git)"), map[string]interface{}{"args": []interface{}{"add", "--", "a b.txt", "c.txt"}})
	if got := typesOf(wf); len(got) != 2 || got[0] != "system.execute_command" || got[1] != "core.code" {
		t.Errorf("git chain: %v", got)
	}
}
