package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/ai/generate"
)

const genToolValid = `{"name":"Fetch","nodes":[
 {"id":"n1","type":"trigger.manual","name":"Start","config":{}},
 {"id":"n2","type":"http.request","name":"Fetch","config":{"url":"https://example.com"}}],
 "connections":[{"id":"c1","source":"n1","source_handle":"main","target":"n2","target_handle":"main"}]}`

const genToolInvalid = `{"name":"Fetch","nodes":[{"id":"n2","type":"http.request","name":"Fetch","config":{}}],"connections":[]}`

func fakeToolExec(t *testing.T, answers ...string) *int {
	t.Helper()
	turns := 0
	orig := generateExecFor
	generateExecFor = func(context.Context, string, string) (generate.ExecFunc, error) {
		return func(context.Context, string, string) (string, error) {
			i := turns
			if i >= len(answers) {
				i = len(answers) - 1
			}
			turns++
			return answers[i], nil
		}, nil
	}
	t.Cleanup(func() { generateExecFor = orig })
	return &turns
}

func TestWorkflowGenerateToolSavesInactiveDraft(t *testing.T) {
	s := newTestServerAllowMutations(t, true)
	turns := fakeToolExec(t, "not json", genToolInvalid, genToolValid)
	text, err := callTool(context.Background(), s, "workflow_generate", json.RawMessage(`{"description":"fetch example.com","runtime":"fake"}`))
	if err != nil {
		t.Fatalf("workflow_generate: %v", err)
	}
	var out struct {
		Saved     bool   `json:"saved"`
		ID        string `json:"id"`
		NodeCount int    `json:"node_count"`
		Attempts  int    `json:"attempts"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	if *turns != 3 || out.Attempts != 3 || !out.Saved || out.ID == "" || out.NodeCount != 2 {
		t.Fatalf("out = %+v (turns %d)", out, *turns)
	}
	got, err := callTool(context.Background(), s, "workflow_get", json.RawMessage(`{"id":"`+out.ID+`"}`))
	if err != nil || !strings.Contains(got, `"is_active": false`) {
		t.Fatalf("saved workflow = %s, %v", got, err)
	}
	listed, err := callTool(context.Background(), s, "workflow_list", nil)
	if err != nil || !strings.Contains(listed, out.ID) {
		t.Errorf("draft not listed for the profile: %s %v", listed, err)
	}
}

func TestWorkflowGenerateToolNoSaveAndGiveUp(t *testing.T) {
	s := newTestServerAllowMutations(t, true)
	fakeToolExec(t, genToolValid)
	text, err := callTool(context.Background(), s, "workflow_generate", json.RawMessage(`{"description":"x","runtime":"fake","save":false}`))
	var top map[string]json.RawMessage
	if err != nil || json.Unmarshal([]byte(text), &top) != nil || string(top["saved"]) != "false" || top["id"] != nil {
		t.Fatalf("save:false = %s, %v", text, err)
	}

	turns := fakeToolExec(t, genToolInvalid)
	_, err = callTool(context.Background(), s, "workflow_generate", json.RawMessage(`{"description":"x","runtime":"fake","max_attempts":2}`))
	if err == nil || !strings.Contains(err.Error(), "2 attempt(s)") || *turns != 2 {
		t.Fatalf("err = %v, turns = %d", err, *turns)
	}
}

func TestWorkflowGenerateToolIsMutating(t *testing.T) {
	s := newTestServerAllowMutations(t, false)
	if _, err := callTool(context.Background(), s, "workflow_generate", json.RawMessage(`{"description":"x"}`)); err == nil || !strings.Contains(err.Error(), "allow-mutations") {
		t.Fatalf("err = %v", err)
	}
}
