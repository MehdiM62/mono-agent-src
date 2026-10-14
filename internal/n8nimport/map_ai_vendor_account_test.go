package n8nimport_test

import (
	"testing"

	"github.com/monoes/mono-agent/internal/n8nimport"
)

// A vendor node or chat model with its vendor credential runs agent.ask on
// the vendor connection (credential_id), not on a signed-in agent CLI.
func TestVendorCredentialRunsOnVendorAccount(t *testing.T) {
	wf, rep := convertNodes(t, `{"name":"Gem","type":"@n8n/n8n-nodes-langchain.googleGemini","typeVersion":1.2,"position":[0,0],
	  "parameters":{"modelId":{"__rl":true,"mode":"list","value":"models/gemini-2.5-pro"},"messages":{"values":[{"content":"hi"}]}},
	  "credentials":{"googlePalmApi":{"id":"1","name":"Gemini"}}}`)
	ask := node(t, wf, "Gem (ask)")
	if ask.Type != "agent.ask" || ask.Config["credential_id"] != "gemini_api" || ask.Config["model"] != "gemini-2.5-pro" {
		t.Fatalf("%s %v\n%s", ask.Type, ask.Config, issueText(rep, "Gem"))
	}
	if _, ok := ask.Config["runtime"]; ok {
		t.Errorf("runtime is set: %v", ask.Config)
	}

	raw := `{"name":"t","nodes":[
	  {"name":"Agent","type":"@n8n/n8n-nodes-langchain.databricksAiAgent","typeVersion":2.2,"position":[0,0],
	   "parameters":{"promptType":"define","text":"hello"}},
	  {"name":"DBX","type":"@n8n/n8n-nodes-langchain.lmChatDatabricks","typeVersion":1,"position":[0,200],
	   "parameters":{"model":{"__rl":true,"mode":"list","value":"my-endpoint"}},
	   "credentials":{"databricksOAuth2Api":{"id":"2","name":"Workspace"}}}],
	 "connections":{"DBX":{"ai_languageModel":[[{"node":"Agent","type":"ai_languageModel","index":0}]]}}}`
	wf, rep, err := n8nimport.Convert([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	m := node(t, wf, "DBX")
	if m.Type != "ai.model" || m.Config["credential_id"] != "databricks" || m.Config["model"] != "my-endpoint" {
		t.Fatalf("%s %v\n%s", m.Type, m.Config, issueText(rep, "Agent"))
	}
	if _, ok := m.Config["runtime"]; ok {
		t.Errorf("runtime is set: %v", m.Config)
	}

	// Without the credential the CLI runtime route stays.
	wf, _ = convertNodes(t, `{"name":"Gem","type":"@n8n/n8n-nodes-langchain.googleGemini","typeVersion":1.2,"position":[0,0],
	  "parameters":{"messages":{"values":[{"content":"hi"}]}}}`)
	if ask := node(t, wf, "Gem (ask)"); ask.Config["runtime"] != "antigravity" || ask.Config["credential_id"] != nil {
		t.Errorf("no credential: %v", ask.Config)
	}
}
