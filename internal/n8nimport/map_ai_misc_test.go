package n8nimport_test

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/n8nimport"
	"github.com/monoes/mono-agent/internal/workflow"
)

// A new OpenAI assistant keeps its instructions and model on the local
// codex agent; an existing one is flagged (its setup lives on OpenAI).
func TestOpenAIAssistantMapsToAgentAsk(t *testing.T) {
	wf, rep := convertNodes(t, `{"name":"Asst","type":"@n8n/n8n-nodes-langchain.openAiAssistant","typeVersion":1.1,"position":[0,0],
	  "parameters":{"mode":"new","name":"Helper","instructions":"Answer in French.","model":"gpt-4o-mini","text":"={{ $json.question }}"}}`)
	a := node(t, wf, "Asst")
	if a.Type != "agent.ask" {
		t.Fatalf("type %s:\n%s", a.Type, issueText(rep, "Asst"))
	}
	wantConfig(t, a, map[string]interface{}{"runtime": "codex", "model": "gpt-4o-mini", "system_prompt": "Answer in French.",
		"output_key": "output", "prompt": "{{ $json.question }}"})
	if nr := nodeReport(t, rep, "Asst"); nr.Status == n8nimport.StatusUnmapped {
		t.Fatalf("status %s", nr.Status)
	}

	_, rep = convertNodes(t, `{"name":"Asst","type":"@n8n/n8n-nodes-langchain.openAiAssistant","typeVersion":1.1,"position":[0,0],
	  "parameters":{"mode":"existing","assistantId":{"__rl":true,"mode":"id","value":"asst_123"},"nativeTools":["code_interpreter"]}}`)
	text := issueText(rep, "Asst")
	if !strings.Contains(text, "live on OpenAI") || !strings.Contains(text, "hosted tools") {
		t.Errorf("existing assistant report:\n%s", text)
	}
}

// Message an Agent asks a local agent once for all items (n8n's default)
// and shapes the answer like n8n: text (v2) / response (v1), and the
// structured output generated from a JSON example.
func TestMessageAnAgent(t *testing.T) {
	wf, rep := convertNodes(t, `{"name":"Msg","type":"n8n-nodes-base.messageAnAgent","typeVersion":2,"position":[0,0],
	  "parameters":{"agentId":{"__rl":true,"mode":"list","value":"ag1"},"message":"={{ $json.q }}",
	    "useStructuredOutput":true,"schemaType":"fromJson","jsonSchemaExample":"{\"city\":\"Paris\",\"temp\":21}"}}`)
	ask := node(t, wf, "Msg (ask)")
	if ask.Type != "agent.ask" {
		t.Fatalf("type %s:\n%s", ask.Type, issueText(rep, "Msg"))
	}
	if ask.Config["execute_once"] != true || ask.Config["prompt"] != "{{ $json.q }}" {
		t.Errorf("config %v", ask.Config)
	}
	schema, _ := ask.Config["output_schema"].(map[string]interface{})
	props, _ := schema["properties"].(map[string]interface{})
	if props["city"] == nil || props["temp"] == nil {
		t.Errorf("output_schema %v", ask.Config["output_schema"])
	}
	shape := node(t, wf, "Msg")
	code, _ := shape.Config["code"].(string)
	if shape.Type != "core.code" || !strings.Contains(code, `out["text"]`) || !strings.Contains(code, "structuredOutput") {
		t.Errorf("shaping node %s: %s", shape.Type, code)
	}
	if !strings.Contains(issueText(rep, "Msg"), "not imported") {
		t.Errorf("report:\n%s", issueText(rep, "Msg"))
	}

	wf, _ = convertNodes(t, `{"name":"Msg","type":"n8n-nodes-base.messageAnAgent","typeVersion":1,"position":[0,0],
	  "parameters":{"message":"hi","advanced":{"invokeMode":"perItem"}}}`)
	if _, ok := node(t, wf, "Msg (ask)").Config["execute_once"]; ok {
		t.Error("per-item invoke mode must run every item")
	}
	if code, _ := node(t, wf, "Msg").Config["code"].(string); !strings.Contains(code, `out["response"]`) {
		t.Errorf("v1 answer key: %s", code)
	}
}

// Plain JavaScript in a LangChain Code node imports as core.code; LangChain
// code (the default) stays a placeholder.
func TestLangChainCode(t *testing.T) {
	wf, rep := convertNodes(t, `{"name":"LC","type":"@n8n/n8n-nodes-langchain.code","typeVersion":1,"position":[0,0],
	  "parameters":{"code":{"execute":{"code":"return $input.all().map(i => ({json: {n: i.json.n * 2}}));"}}}}`)
	if lc := node(t, wf, "LC"); lc.Type != "core.code" {
		t.Fatalf("plain JS: type %s\n%s", lc.Type, issueText(rep, "LC"))
	}
	_, rep = convertNodes(t, `{"name":"LC","type":"@n8n/n8n-nodes-langchain.code","typeVersion":1,"position":[0,0],
	  "parameters":{"code":{"execute":{"code":"const llm = await this.getInputConnectionData('ai_languageModel', 0); return [];"}}}}`)
	if nr := nodeReport(t, rep, "LC"); nr.Status != n8nimport.StatusUnmapped {
		t.Errorf("LangChain code: status %s", nr.Status)
	}
}

// The Tool Executor runs its attached tool with the query's arguments: a
// code step builds the item from the query, and an app "…Tool" node maps as
// its app node with $fromAI(key) read from the item.
func TestToolExecutorRunsTheAttachedTool(t *testing.T) {
	raw := `{"name":"t","nodes":[
	  {"name":"Exec","type":"@n8n/n8n-nodes-langchain.toolExecutor","typeVersion":1,"position":[0,0],
	   "parameters":{"query":"{\"Echo_Code\":{\"text\":\"hello\"}}","toolName":"Echo_Code"}},
	  {"name":"Echo Code","type":"@n8n/n8n-nodes-langchain.toolCode","typeVersion":1.3,"position":[0,200],
	   "parameters":{"jsCode":"return query;"}},
	  {"name":"Set Tool","type":"n8n-nodes-base.setTool","typeVersion":3.4,"position":[0,400],
	   "parameters":{"assignments":{"assignments":[{"id":"a","name":"out","value":"={{ $fromAI('text') }}","type":"string"}]}}}],
	 "connections":{"Set Tool":{"ai_tool":[[{"node":"Exec","type":"ai_tool","index":0}]]}}}`
	wf, rep, err := n8nimport.Convert([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	q := node(t, wf, "Exec (query)")
	if q.Type != "core.code" {
		t.Fatalf("query step %s\n%s", q.Type, issueText(rep, "Exec"))
	}
	set := node(t, wf, "Exec")
	if set.Type != "core.set" {
		t.Fatalf("tool step %s\n%s", set.Type, issueText(rep, "Exec"))
	}
	if !strings.Contains(fmtConfig(set), `$json["text"]`) && !strings.Contains(fmtConfig(set), `$json.text`) {
		t.Errorf("$fromAI not read from the item: %v", set.Config)
	}
	if !connected(wf, q.ID, set.ID) {
		t.Errorf("query step is not wired to the tool")
	}

	_, rep = convertNodes(t, `{"name":"Exec","type":"@n8n/n8n-nodes-langchain.toolExecutor","typeVersion":1,"position":[0,0],"parameters":{}}`)
	if nr := nodeReport(t, rep, "Exec"); nr.Status == n8nimport.StatusUnmapped {
		t.Errorf("no tool: status %s", nr.Status)
	}
}

func fmtConfig(n workflow.WorkflowFileNode) string {
	var b strings.Builder
	for k, v := range n.Config {
		b.WriteString(k + "=")
		b.WriteString(strings.TrimSpace(strings.ReplaceAll(toString(v), "\n", " ")))
		b.WriteString(";")
	}
	return b.String()
}

func toString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case []interface{}:
		var parts []string
		for _, x := range t {
			parts = append(parts, toString(x))
		}
		return strings.Join(parts, ",")
	case map[string]interface{}:
		var parts []string
		for k, x := range t {
			parts = append(parts, k+":"+toString(x))
		}
		return strings.Join(parts, ",")
	}
	return ""
}

func connected(wf *workflow.WorkflowFile, from, to string) bool {
	for _, c := range wf.Connections {
		if c.Source == from && c.Target == to {
			return true
		}
	}
	return false
}

// Message an Agent's session is kept in the local chat memory: v3.1 from the
// chat trigger's sessionId (or a defined key), earlier versions from
// advanced.sessionId; no session → a fresh conversation every run.
func TestMessageAnAgentSession(t *testing.T) {
	wf, _ := convertNodes(t, `{"name":"Msg","type":"n8n-nodes-base.messageAnAgent","typeVersion":3.1,"position":[0,0],
	  "parameters":{"message":"hi"}}`)
	mem, _ := node(t, wf, "Msg (ask)").Config["memory"].(map[string]interface{})
	if mem["session_key"] != "{{ $json.sessionId }}" || mem["skip_when_missing"] != true {
		t.Errorf("v3.1 default: memory %v", mem)
	}
	wf, _ = convertNodes(t, `{"name":"Msg","type":"n8n-nodes-base.messageAnAgent","typeVersion":3.1,"position":[0,0],
	  "parameters":{"message":"hi","advanced":{"session":{"session":{"sessionIdType":"customKey","sessionKey":"={{ $json.user }}"}}}}}`)
	if mem, _ := node(t, wf, "Msg (ask)").Config["memory"].(map[string]interface{}); mem["session_key"] != "{{ $json.user }}" {
		t.Errorf("custom key: memory %v", mem)
	}
	wf, _ = convertNodes(t, `{"name":"Msg","type":"n8n-nodes-base.messageAnAgent","typeVersion":2,"position":[0,0],
	  "parameters":{"message":"hi","advanced":{"sessionId":"support-42"}}}`)
	if mem, _ := node(t, wf, "Msg (ask)").Config["memory"].(map[string]interface{}); mem["session_key"] != "support-42" {
		t.Errorf("v2 sessionId: memory %v", mem)
	}
	wf, _ = convertNodes(t, `{"name":"Msg","type":"n8n-nodes-base.messageAnAgent","typeVersion":2,"position":[0,0],"parameters":{"message":"hi"}}`)
	if _, ok := node(t, wf, "Msg (ask)").Config["memory"]; ok {
		t.Error("v2 without a session must not keep memory")
	}
}

// The executed tool's $fromAI goes through the expression translator, so a
// default value survives (the item may not carry the key).
func TestToolExecutorKeepsFromAIDefault(t *testing.T) {
	raw := `{"name":"t","nodes":[
	  {"name":"Exec","type":"@n8n/n8n-nodes-langchain.toolExecutor","typeVersion":1,"position":[0,0],
	   "parameters":{"query":"{}"}},
	  {"name":"Set Tool","type":"n8n-nodes-base.setTool","typeVersion":3.4,"position":[0,400],
	   "parameters":{"assignments":{"assignments":[{"id":"a","name":"out","value":"={{ $fromAI('text', '', 'string', 'fallback') }}","type":"string"}]}}}],
	 "connections":{"Set Tool":{"ai_tool":[[{"node":"Exec","type":"ai_tool","index":0}]]}}}`
	wf, rep, err := n8nimport.Convert([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	cfg := fmtConfig(node(t, wf, "Exec"))
	if !strings.Contains(cfg, "text") || !strings.Contains(cfg, "fallback") {
		t.Errorf("$fromAI default lost: %s\n%s", cfg, issueText(rep, "Exec"))
	}
}
