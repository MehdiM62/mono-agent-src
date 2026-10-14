package n8nimport_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/n8nimport"
	"github.com/monoes/mono-agent/internal/noderegistry"
	"github.com/monoes/mono-agent/internal/workflow"
)

// agentWithTools is an n8n AI Agent with an OpenRouter model and tools of
// every kind: a generated tool variant (slackTool), the HTTP Request tool,
// a code tool, the calculator, an MCP client and a sub-workflow.
const agentWithTools = `{"name":"t","nodes":[
 {"name":"Start","type":"n8n-nodes-base.manualTrigger","typeVersion":1,"position":[0,0],"parameters":{}},
 {"name":"Agent","type":"@n8n/n8n-nodes-langchain.agent","typeVersion":2,"position":[200,0],
  "parameters":{"promptType":"define","text":"={{ $json.question }}"}},
 {"name":"Model","type":"@n8n/n8n-nodes-langchain.lmChatOpenRouter","typeVersion":1,"position":[100,200],
  "parameters":{"model":"openai/gpt-4o-mini"}},
 {"name":"Notify","type":"n8n-nodes-base.slackTool","typeVersion":2.3,"position":[200,200],
  "parameters":{"select":"channel","channelId":{"__rl":true,"mode":"id","value":"C1"},
   "text":"={{ $fromAI('message', 'what to post', 'string') }}","descriptionType":"manual","toolDescription":"Post to Slack"}},
 {"name":"Orders","type":"@n8n/n8n-nodes-langchain.toolHttpRequest","typeVersion":1.1,"position":[300,200],
  "parameters":{"toolDescription":"Look up an order","url":"https://api.example.com/orders/{orderId}","sendQuery":true,
   "parametersQuery":{"values":[{"name":"status","valueProvider":"modelOptional"},{"name":"limit","valueProvider":"fieldValue","value":"5"}]},
   "placeholderDefinitions":{"values":[{"name":"orderId","type":"string"}]}}},
 {"name":"Shout","type":"@n8n/n8n-nodes-langchain.toolCode","typeVersion":1.3,"position":[400,200],
  "parameters":{"name":"shout","description":"upper-cases","jsCode":"return query.toUpperCase();"}},
 {"name":"Calc","type":"@n8n/n8n-nodes-langchain.toolCalculator","typeVersion":1,"position":[500,200],"parameters":{}},
 {"name":"MCP","type":"@n8n/n8n-nodes-langchain.mcpClientTool","typeVersion":1.2,"position":[600,200],
  "parameters":{"endpointUrl":"https://mcp.example.com/mcp","include":"selected","includeTools":["search"]}},
 {"name":"Sub","type":"@n8n/n8n-nodes-langchain.toolWorkflow","typeVersion":2.2,"position":[700,200],
  "parameters":{"description":"runs the lookup","workflowId":{"__rl":true,"mode":"list","value":"9","cachedResultName":"Lookup"},
   "workflowInputs":{"mappingMode":"defineBelow","value":{"sku":"={{ $fromAI('sku', '', 'string') }}"},
    "schema":[{"id":"sku","type":"string"}]}}}
],"connections":{
 "Start":{"main":[[{"node":"Agent","type":"main","index":0}]]},
 "Model":{"ai_languageModel":[[{"node":"Agent","type":"ai_languageModel","index":0}]]},
 "Notify":{"ai_tool":[[{"node":"Agent","type":"ai_tool","index":0}]]},
 "Orders":{"ai_tool":[[{"node":"Agent","type":"ai_tool","index":0}]]},
 "Shout":{"ai_tool":[[{"node":"Agent","type":"ai_tool","index":0}]]},
 "Calc":{"ai_tool":[[{"node":"Agent","type":"ai_tool","index":0}]]},
 "MCP":{"ai_tool":[[{"node":"Agent","type":"ai_tool","index":0}]]},
 "Sub":{"ai_tool":[[{"node":"Agent","type":"ai_tool","index":0}]]}
}}`

func TestAgentToolsAreKeptAsDisabledNodes(t *testing.T) {
	wf, rep, err := n8nimport.Convert([]byte(agentWithTools))
	if err != nil {
		t.Fatal(err)
	}
	model := attachedSub(t, wf, "Agent", "ai_languageModel")
	if model.Type != "ai.model" || model.Name != "Model" || model.Disabled {
		t.Errorf("model sub-node %s %q disabled %v", model.Type, model.Name, model.Disabled)
	}
	wantConfig(t, model, map[string]interface{}{"runtime": "opencode", "model": "openrouter/openai/gpt-4o-mini"})

	want := map[string]string{"Notify": "comm.slack", "Orders": "http.request", "Shout": "core.code", "Calc": "core.code",
		"MCP": "mcp.call", "Sub": "core.execute_workflow"}
	ids := map[string]bool{}
	for name, typ := range want {
		n := node(t, wf, name)
		if n.Type != typ {
			t.Errorf("%s: type %s, want %s", name, n.Type, typ)
		}
		if !n.Disabled {
			t.Errorf("%s is not disabled", name)
		}
		ids[n.ID] = true
	}
	ids[node(t, wf, "Sub (inputs)").ID] = true // the sub-workflow call's own chain
	for _, c := range wf.Connections {
		if ids[c.Source] != ids[c.Target] {
			t.Errorf("tool node is connected: %+v", c)
		}
	}
	if got := node(t, wf, "Notify").Config["text"]; got != "{{ $json.message }}" {
		t.Errorf("slack text %v", got)
	}
	orders := node(t, wf, "Orders")
	wantConfig(t, orders, map[string]interface{}{"url": "https://api.example.com/orders/{{ $json.orderId }}", "method": "GET"})
	if q := jsonString(orders.Config["query_params"]); !strings.Contains(q, `{{ $json.status }}`) || !strings.Contains(q, `"5"`) {
		t.Errorf("query params %s", q)
	}
	wantConfig(t, node(t, wf, "MCP"), map[string]interface{}{"url": "https://mcp.example.com/mcp", "operation": "call_tool",
		"tool": "{{ $json.tool }}"})
	wantConfig(t, node(t, wf, "Sub"), map[string]interface{}{"workflowId": "Lookup"})
	if in := jsonString(node(t, wf, "Sub (inputs)").Config["assignments"]); !strings.Contains(in, "{{ $json.sku }}") {
		t.Errorf("sub-workflow inputs %s", in)
	}

	for _, nr := range rep.Nodes {
		switch nr.Name {
		case "Agent":
			text := issueText(rep, "Agent")
			for name := range want {
				if !strings.Contains(text, "kept as the disabled node \""+name+"\"") {
					t.Errorf("agent report does not name tool %s:\n%s", name, text)
				}
			}
			if nr.Status != n8nimport.StatusPartial {
				t.Errorf("agent status %s", nr.Status)
			}
		case "Notify", "Orders", "Shout", "Calc", "MCP":
			if nr.Status == n8nimport.StatusUnmapped || nr.Status == n8nimport.StatusFolded {
				t.Errorf("%s status %s", nr.Name, nr.Status)
			}
		case "Model":
			if nr.Status != n8nimport.StatusFull || nr.MonoType != "ai.model" || nr.MonoID != model.ID {
				t.Errorf("model status %s type %s id %s", nr.Status, nr.MonoType, nr.MonoID)
			}
		}
	}
	if rep.Counts.Unmapped != 0 {
		t.Errorf("unmapped %d\n%s", rep.Counts.Unmapped, rep.Text())
	}
	b, _ := json.Marshal(wf)
	parsed, err := workflow.ParseWorkflowFileBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := workflow.ValidateForActivation(&parsed); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestToolCodeCalculatorAndThinkRun(t *testing.T) {
	wf, _, err := n8nimport.Convert([]byte(agentWithTools))
	if err != nil {
		t.Fatal(err)
	}
	out := runNode(t, wf, "Shout", []map[string]interface{}{{"input": "hi"}})
	expectJSON(t, "code tool", out, `[{"response":"HI"}]`)
	out = runNode(t, wf, "Calc", []map[string]interface{}{{"input": "2^3 + sqrt(16) * pi / PI"}})
	expectJSON(t, "calculator", out, `[{"response":"12"}]`)

	think, _ := convertNodes(t, `{"name":"Think","type":"@n8n/n8n-nodes-langchain.toolThink","typeVersion":1,"position":[0,0],"parameters":{}}`)
	expectJSON(t, "think", runNode(t, think, "Think", []map[string]interface{}{{"input": "plan"}}), `[{"response":"plan"}]`)
}

func TestCalculatorRefusesCode(t *testing.T) {
	wf, _, err := n8nimport.Convert([]byte(agentWithTools))
	if err != nil {
		t.Fatal(err)
	}
	node := node(t, wf, "Calc")
	for _, bad := range []string{"constructor", "this", "1; x = 2", "'a'"} {
		if _, err := runCode(t, node, []map[string]interface{}{{"input": bad}}); err == nil {
			t.Errorf("calculator accepted %q", bad)
		}
	}
}

// A tool variant in the main flow runs like its base node.
func TestToolVariantStandalone(t *testing.T) {
	params := `{"select":"channel","channelId":{"__rl":true,"mode":"id","value":"C1"},"text":"hi","toolDescription":"x","descriptionType":"manual"}`
	tool, rep := convertNodes(t, `{"name":"S","type":"n8n-nodes-base.slackTool","typeVersion":2.3,"position":[0,0],"parameters":`+params+`}`)
	base, _ := convertNodes(t, `{"name":"S","type":"n8n-nodes-base.slack","typeVersion":2.3,"position":[0,0],"parameters":`+params+`}`)
	a, b := node(t, tool, "S"), node(t, base, "S")
	if a.Type != b.Type || jsonString(a.Config) != jsonString(b.Config) || a.Disabled {
		t.Fatalf("tool variant %s %v, base %s %v", a.Type, a.Config, b.Type, b.Config)
	}
	if text := issueText(rep, "S"); strings.Contains(text, "toolDescription") {
		t.Errorf("tool parameters reported: %s", text)
	}
}

func TestChatModelRoutes(t *testing.T) {
	cases := []struct {
		typ, model, runtime, wantModel string
		partial                        bool
	}{
		{"lmChatGoogleGemini", "models/gemini-2.5-flash", "antigravity", "gemini-2.5-flash", false},
		{"lmChatXAiGrok", "grok-4", "grok", "grok-4", false},
		{"lmChatMoonshot", "kimi-k2", "kimicode", "kimi-k2", false},
		{"lmChatDeepSeek", "deepseek-chat", "dsh", "deepseek-chat", false},
		{"lmChatGroq", "llama-3.3-70b-versatile", "opencode", "groq/llama-3.3-70b-versatile", false},
		{"lmChatAwsBedrock", "anthropic.claude-3-5-sonnet", "opencode", "amazon-bedrock/anthropic.claude-3-5-sonnet", false},
		{"lmChatCohere", "command-r", "claude", "", true},
		{"lmChatAzureOpenAi", "my-deployment", "codex", "my-deployment", true},
	}
	for _, tc := range cases {
		src := `{"name":"t","nodes":[
		 {"name":"Chain","type":"@n8n/n8n-nodes-langchain.chainLlm","typeVersion":1.5,"position":[0,0],"parameters":{"promptType":"define","text":"hi"}},
		 {"name":"M","type":"@n8n/n8n-nodes-langchain.` + tc.typ + `","typeVersion":1,"position":[0,200],"parameters":{"model":"` + tc.model + `"}}],
		 "connections":{"M":{"ai_languageModel":[[{"node":"Chain","type":"ai_languageModel","index":0}]]}}}`
		wf, rep, err := n8nimport.Convert([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		m := attachedSub(t, wf, "Chain", "ai_languageModel").Config
		if m["runtime"] != tc.runtime || (tc.wantModel != "" && m["model"] != tc.wantModel) ||
			(tc.wantModel == "" && m["model"] != nil) {
			t.Errorf("%s: runtime %v model %v", tc.typ, m["runtime"], m["model"])
		}
		if _, set := node(t, wf, "Chain").Config["runtime"]; set {
			t.Errorf("%s: runtime set on the chain", tc.typ)
		}
		for _, nr := range rep.Nodes {
			if nr.Name == "Chain" && (nr.Status == n8nimport.StatusPartial) != tc.partial {
				t.Errorf("%s: status %s\n%s", tc.typ, nr.Status, issueText(rep, "Chain"))
			}
		}
	}
}

func TestModelSelectorUsesFirstModel(t *testing.T) {
	src := `{"name":"t","nodes":[
	 {"name":"Agent","type":"@n8n/n8n-nodes-langchain.agent","typeVersion":2,"position":[0,0],"parameters":{"promptType":"define","text":"hi"}},
	 {"name":"Pick","type":"@n8n/n8n-nodes-langchain.modelSelector","typeVersion":1,"position":[0,200],"parameters":{}},
	 {"name":"Claude","type":"@n8n/n8n-nodes-langchain.lmChatAnthropic","typeVersion":1.3,"position":[0,400],
	  "parameters":{"model":{"__rl":true,"mode":"list","value":"claude-sonnet-4-5"}}}],
	 "connections":{"Pick":{"ai_languageModel":[[{"node":"Agent","type":"ai_languageModel","index":0}]]},
	  "Claude":{"ai_languageModel":[[{"node":"Pick","type":"ai_languageModel","index":0}]]}}}`
	wf, rep, err := n8nimport.Convert([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	m := attachedSub(t, wf, "Agent", "ai_languageModel")
	if m.Name != "Pick" || m.Type != "ai.model" {
		t.Errorf("model sub-node %s %q", m.Type, m.Name)
	}
	wantConfig(t, m, map[string]interface{}{"runtime": "claude", "model": "claude-sonnet-4-5"})
	if nr := nodeReport(t, rep, "Claude"); nr.Status != n8nimport.StatusFolded {
		t.Errorf("selected model status %s", nr.Status)
	}
	if !strings.Contains(issueText(rep, "Agent"), "rules of modelSelector are not applied") {
		t.Errorf("report:\n%s", issueText(rep, "Agent"))
	}
}

func TestFromAIExpressions(t *testing.T) {
	wf, _ := convertNodes(t, `{"name":"Set","type":"n8n-nodes-base.set","typeVersion":3.4,"position":[0,0],"parameters":{
	  "assignments":{"assignments":[
	   {"id":"1","name":"a","value":"={{ $fromAI('first_name') }}","type":"string"},
	   {"id":"2","name":"b","value":"={{ $fromAI('limit', 'how many', 'number', 10) }}","type":"string"},
	   {"id":"3","name":"c","value":"={{ $fromAi('dash-key') }}","type":"string"}]}}}`)
	out := runNode(t, wf, "Set", []map[string]interface{}{{"first_name": "Ada", "dash-key": "d"}})
	expectJSON(t, "fromAI", out, `[{"a":"Ada","b":"10","c":"d"}]`)
}

// runCode runs a core.code node on items and returns the executor's error.
func runCode(t *testing.T, n workflow.WorkflowFileNode, items []map[string]interface{}) ([]workflow.NodeOutput, error) {
	t.Helper()
	cfg, err := workflow.NewExpressionEngine().ResolveConfig(n.Config, workflow.ExpressionContext{JSON: items[0]})
	if err != nil {
		t.Fatal(err)
	}
	factory, ok := noderegistry.Build(nil).Get(n.Type)
	if !ok {
		t.Fatalf("type %s not registered", n.Type)
	}
	in := make([]workflow.Item, len(items))
	for i, it := range items {
		in[i] = workflow.NewItem(it)
	}
	return factory().Execute(context.Background(), workflow.NodeInput{Items: in}, cfg)
}

func TestVectorStoreAndSearchTools(t *testing.T) {
	src := `{"name":"t","nodes":[
	 {"name":"Agent","type":"@n8n/n8n-nodes-langchain.agent","typeVersion":2,"position":[0,0],"parameters":{"promptType":"define","text":"hi"}},
	 {"name":"KB","type":"@n8n/n8n-nodes-langchain.toolVectorStore","typeVersion":1.1,"position":[0,200],"parameters":{"description":"docs","topK":3}},
	 {"name":"Store","type":"@n8n/n8n-nodes-langchain.vectorStoreInMemory","typeVersion":1.3,"position":[0,400],"parameters":{"mode":"retrieve","memoryKey":"kb"}},
	 {"name":"Emb","type":"@n8n/n8n-nodes-langchain.embeddingsOllama","typeVersion":1,"position":[0,600],"parameters":{"model":"nomic-embed-text"}},
	 {"name":"Search","type":"@n8n/n8n-nodes-langchain.toolSerpApi","typeVersion":1,"position":[200,200],"parameters":{"options":{"gl":"de"}},
	  "credentials":{"serpApi":{"id":"1","name":"Serp"}}},
	 {"name":"Wiki","type":"@n8n/n8n-nodes-langchain.toolWikipedia","typeVersion":1,"position":[400,200],"parameters":{}}],
	 "connections":{"KB":{"ai_tool":[[{"node":"Agent","type":"ai_tool","index":0}]]},
	  "Search":{"ai_tool":[[{"node":"Agent","type":"ai_tool","index":0}]]},
	  "Wiki":{"ai_tool":[[{"node":"Agent","type":"ai_tool","index":0}]]},
	  "Store":{"ai_vectorStore":[[{"node":"KB","type":"ai_vectorStore","index":0}]]},
	  "Emb":{"ai_embedding":[[{"node":"Store","type":"ai_embedding","index":0}]]}}}`
	wf, rep, err := n8nimport.Convert([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	kb := node(t, wf, "KB")
	if kb.Type != "vector.store" || !kb.Disabled {
		t.Fatalf("KB %s %v", kb.Type, kb.Config)
	}
	wantConfig(t, kb, map[string]interface{}{"provider": "memory", "memory_key": "kb", "query": "{{ $json.input }}", "top_k": 3})
	emb := attachedSub(t, wf, "KB", "ai_embedding")
	if emb.Type != "vector.embeddings" || emb.Name != "Emb" || emb.Config["provider"] != "ollama" {
		t.Errorf("embeddings %s %q %v", emb.Type, emb.Name, emb.Config)
	}
	wantConfig(t, node(t, wf, "Search"), map[string]interface{}{"url": "https://serpapi.com/search.json",
		"query_params": map[string]interface{}{"engine": "google", "q": "{{ default $json.query $json.input }}", "gl": "de",
			"api_key": "@secret:serp_api_key"}})
	wantConfig(t, node(t, wf, "Wiki"), map[string]interface{}{"url": "https://en.wikipedia.org/w/api.php"})
	if rep.Counts.Unmapped != 0 {
		t.Errorf("unmapped:\n%s", rep.Text())
	}
}

// A Tool Executor runs its tool in its own chain; the tool is not kept a
// second time as a disabled node.
func TestToolExecutorToolIsNotKeptTwice(t *testing.T) {
	src := `{"name":"t","nodes":[
	 {"name":"Run","type":"@n8n/n8n-nodes-langchain.toolExecutor","typeVersion":1,"position":[0,0],"parameters":{"query":"{\"Calc\":{\"input\":\"1+1\"}}"}},
	 {"name":"Calc","type":"@n8n/n8n-nodes-langchain.toolCalculator","typeVersion":1,"position":[0,200],"parameters":{}}],
	 "connections":{"Calc":{"ai_tool":[[{"node":"Run","type":"ai_tool","index":0}]]}}}`
	wf, _, err := n8nimport.Convert([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range wf.Nodes {
		if n.Disabled || n.Name == "Calc" && n.Type != "core.code" {
			t.Errorf("unexpected node %s (%s, disabled %v)", n.Name, n.Type, n.Disabled)
		}
	}
}

// n8n's Google service-account credential on Vertex embeddings becomes the
// google_service_account connection.
func TestVertexEmbeddingsUseServiceAccountConnection(t *testing.T) {
	src := `{"name":"t","nodes":[
	 {"name":"Store","type":"@n8n/n8n-nodes-langchain.vectorStoreInMemory","typeVersion":1.3,"position":[0,0],"parameters":{"mode":"insert","memoryKey":"kb"}},
	 {"name":"Emb","type":"@n8n/n8n-nodes-langchain.embeddingsGoogleVertex","typeVersion":1,"position":[0,200],
	  "parameters":{"projectId":{"__rl":true,"mode":"id","value":"proj"},"modelName":"text-embedding-005"},
	  "credentials":{"googleApi":{"id":"7","name":"GCP SA"}}}],
	 "connections":{"Emb":{"ai_embedding":[[{"node":"Store","type":"ai_embedding","index":0}]]}}}`
	wf, rep, err := n8nimport.Convert([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	emb := attachedSub(t, wf, "Store", "ai_embedding").Config
	if emb["provider"] != "vertex" || emb["project_id"] != "proj" || emb["credential_id"] != "google_service_account" {
		t.Fatalf("embedding %v", emb)
	}
	found := false
	for _, c := range rep.Credentials {
		if c.Platform == "google_service_account" && strings.Contains(c.Hint, "privateKey → private_key") {
			found = true
		}
	}
	if !found {
		t.Errorf("credentials %+v", rep.Credentials)
	}
	if text := issueText(rep, "Store"); strings.Contains(text, "service-account credential is not carried over") {
		t.Errorf("report still says the credential is lost:\n%s", text)
	}
}
