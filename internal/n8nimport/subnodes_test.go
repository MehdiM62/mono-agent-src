package n8nimport_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/n8nimport"
	"github.com/monoes/mono-agent/internal/noderegistry"
	"github.com/monoes/mono-agent/internal/testdb"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// validateClean parses the converted workflow and fails on any validation
// error (sub-node types are registered, ai_… connections accepted).
func validateClean(t *testing.T, wf *workflow.WorkflowFile, rep *n8nimport.Report) workflow.Workflow {
	t.Helper()
	raw, err := json.Marshal(wf)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := workflow.ParseWorkflowFileBytes(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	res := workflow.ValidateWorkflow(&parsed, workflow.ValidateOptions{KnownNodeType: noderegistry.Build(nil).Has})
	for _, is := range res.Issues {
		if is.Severity == "error" || is.Code == "unknown_field" || is.Code == "invalid_option" {
			t.Errorf("validation issue: %+v", is)
		}
	}
	if len(rep.Validation) != 0 || !rep.Complete {
		t.Errorf("report validation %v complete %v", rep.Validation, rep.Complete)
	}
	if _, err := workflow.BuildDAG(parsed.Nodes, parsed.Connections); err != nil {
		t.Errorf("dag: %v", err)
	}
	return parsed
}

const agentSubNodes = `{"name":"chat","nodes":[
 {"id":"t","name":"Chat","type":"@n8n/n8n-nodes-langchain.chatTrigger","typeVersion":1.1,"position":[0,0],"parameters":{}},
 {"id":"a","name":"Agent","type":"@n8n/n8n-nodes-langchain.agent","typeVersion":2,"position":[200,0],"parameters":{"hasOutputParser":true}},
 {"id":"m","name":"GPT","type":"@n8n/n8n-nodes-langchain.lmChatOpenAi","typeVersion":1.2,"position":[100,200],
  "parameters":{"model":{"__rl":true,"mode":"list","value":"gpt-4o-mini"}},"credentials":{"openAiApi":{"id":"1","name":"OpenAI"}}},
 {"id":"mem","name":"Window","type":"@n8n/n8n-nodes-langchain.memoryBufferWindow","typeVersion":1.3,"position":[200,200],
  "parameters":{"contextWindowLength":4}},
 {"id":"p","name":"Parser","type":"@n8n/n8n-nodes-langchain.outputParserStructured","typeVersion":1.3,"position":[300,200],
  "parameters":{"jsonSchemaExample":"{\"answer\": \"text\", \"score\": 1}"}},
 {"id":"c","name":"Calc","type":"@n8n/n8n-nodes-langchain.toolCalculator","typeVersion":1,"position":[400,200],"parameters":{}}
],"connections":{
 "Chat":{"main":[[{"node":"Agent","type":"main","index":0}]]},
 "GPT":{"ai_languageModel":[[{"node":"Agent","type":"ai_languageModel","index":0}]]},
 "Window":{"ai_memory":[[{"node":"Agent","type":"ai_memory","index":0}]]},
 "Parser":{"ai_outputParser":[[{"node":"Agent","type":"ai_outputParser","index":0}]]},
 "Calc":{"ai_tool":[[{"node":"Agent","type":"ai_tool","index":0}]]}
}}`

// An agent's chat model, memory and output parser become ai.model /
// ai.memory / ai.output_parser nodes attached over n8n's connection types;
// the agent's config holds none of them, and the result validates.
func TestAgentSubNodesImportAsNodes(t *testing.T) {
	wf, rep, err := n8nimport.Convert([]byte(agentSubNodes))
	if err != nil {
		t.Fatal(err)
	}
	agent := node(t, wf, "Agent")
	if agent.Type != "agent.ask" {
		t.Fatalf("agent type %s", agent.Type)
	}
	for _, k := range []string{"runtime", "model", "memory", "output_schema"} {
		if v, set := agent.Config[k]; set {
			t.Errorf("agent config %s = %v: the sub-node is folded in", k, v)
		}
	}
	for _, c := range []struct {
		kind, name, typ string
		want            map[string]interface{}
	}{
		{"ai_languageModel", "GPT", "ai.model", map[string]interface{}{"runtime": "codex", "model": "gpt-4o-mini"}},
		{"ai_memory", "Window", "ai.memory", map[string]interface{}{"session_key": "{{ $json.sessionId }}", "window": 8}},
		{"ai_outputParser", "Parser", "ai.output_parser", map[string]interface{}{"schema_type": "example",
			"json_example": `{"answer": "text", "score": 1}`}},
	} {
		n := attachedSub(t, wf, "Agent", c.kind)
		if n.Type != c.typ || n.Name != c.name || n.Disabled {
			t.Errorf("%s: %s %q disabled %v", c.kind, n.Type, n.Name, n.Disabled)
		}
		wantConfig(t, n, c.want)
		if nr := nodeReport(t, rep, c.name); nr.Status != n8nimport.StatusFull || nr.MonoType != c.typ || nr.MonoID != n.ID {
			t.Errorf("%s report: %s %s %s\n%s", c.name, nr.Status, nr.MonoType, nr.MonoID, issueText(rep, c.name))
		}
	}
	// The tool stays a disabled node beside the agent: agent.ask takes no
	// ai_tool connection.
	calc := node(t, wf, "Calc")
	for _, c := range wf.Connections {
		if c.SourceHandle == "ai_tool" || c.TargetHandle == "ai_tool" || c.Source == calc.ID || c.Target == calc.ID {
			t.Errorf("tool connection %+v", c)
		}
	}
	if !calc.Disabled {
		t.Error("tool is not disabled")
	}
	text := issueText(rep, "Agent")
	if !strings.Contains(text, "kept as the disabled node \"Calc\"") || !strings.Contains(text, "local chat memory") {
		t.Errorf("agent report:\n%s", text)
	}
	if strings.Contains(text, "not applied") {
		t.Errorf("the output parser is reported as dropped:\n%s", text)
	}
	if rep.Counts.Folded != 0 {
		t.Errorf("folded %d", rep.Counts.Folded)
	}
	validateClean(t, wf, rep)
}

// The parser versions: a JSON Schema up to 1.1, manual or example later;
// an auto-fixing parser keeps its inner parser's schema; others are not
// applied.
func TestOutputParserVariants(t *testing.T) {
	for _, c := range []struct {
		name, typ string
		version   float64
		params    string
		inner     string // an inner structured parser's parameters (auto-fixing)
		want      map[string]interface{}
		warn      string
	}{
		{"v1.1 schema", "outputParserStructured", 1.1, `{"jsonSchema":"{\"type\":\"object\"}"}`, "",
			map[string]interface{}{"schema_type": "schema", "json_schema": `{"type":"object"}`}, ""},
		{"manual", "outputParserStructured", 1.2, `{"schemaType":"manual","inputSchema":"{\"type\":\"array\"}"}`, "",
			map[string]interface{}{"schema_type": "schema", "json_schema": `{"type":"array"}`}, ""},
		{"default example", "outputParserStructured", 1.2, `{}`, "",
			map[string]interface{}{"schema_type": "example"}, ""},
		{"auto-fixing", "outputParserAutofixing", 1, `{}`, `{"jsonSchemaExample":"{\"x\": 1}"}`,
			map[string]interface{}{"schema_type": "example", "json_example": `{"x": 1}`}, "not sent back to a model"},
		{"item list", "outputParserItemList", 1, `{}`, "", nil, "is not applied"},
	} {
		t.Run(c.name, func(t *testing.T) {
			nodes := `{"id":"a","name":"Chain","type":"@n8n/n8n-nodes-langchain.chainLlm","typeVersion":1.5,"position":[0,0],"parameters":{"promptType":"define","text":"hi"}},
			 {"id":"p","name":"P","type":"@n8n/n8n-nodes-langchain.` + c.typ + `","typeVersion":` + fmt.Sprint(c.version) + `,"position":[0,200],"parameters":` + c.params + `}`
			conns := `"P":{"ai_outputParser":[[{"node":"Chain","type":"ai_outputParser","index":0}]]}`
			if c.inner != "" {
				nodes += `,{"id":"i","name":"Inner","type":"@n8n/n8n-nodes-langchain.outputParserStructured","typeVersion":1.3,"position":[0,400],"parameters":` + c.inner + `}`
				conns += `,"Inner":{"ai_outputParser":[[{"node":"P","type":"ai_outputParser","index":0}]]}`
			}
			wf, rep, err := n8nimport.Convert([]byte(`{"name":"p","nodes":[` + nodes + `],"connections":{` + conns + `}}`))
			if err != nil {
				t.Fatal(err)
			}
			text := issueText(rep, "Chain")
			if c.warn != "" && !strings.Contains(text, c.warn) {
				t.Errorf("report lacks %q:\n%s", c.warn, text)
			}
			if c.want == nil {
				for _, conn := range wf.Connections {
					if conn.TargetHandle == "ai_outputParser" {
						t.Errorf("parser attached: %+v", conn)
					}
				}
				if nr := nodeReport(t, rep, "P"); nr.Status != n8nimport.StatusFolded {
					t.Errorf("unused parser status %s", nr.Status)
				}
				return
			}
			p := attachedSub(t, wf, "Chain", "ai_outputParser")
			if p.Type != "ai.output_parser" || p.Name != "P" {
				t.Errorf("parser %s %q", p.Type, p.Name)
			}
			wantConfig(t, p, c.want)
			if c.name == "default example" && !strings.Contains(fmt.Sprint(p.Config["json_example"]), "California") {
				t.Errorf("default example %v", p.Config["json_example"])
			}
		})
	}
}

// One n8n memory feeding an agent and a Chat Memory Manager becomes one
// ai.memory node attached to both.
func TestSharedMemorySubNodeIsOneNode(t *testing.T) {
	src := `{"name":"m","nodes":[
	 {"id":"t","name":"Start","type":"n8n-nodes-base.manualTrigger","typeVersion":1,"position":[0,0],"parameters":{}},
	 {"id":"a","name":"Agent","type":"@n8n/n8n-nodes-langchain.agent","typeVersion":2,"position":[200,0],"parameters":{"promptType":"define","text":"hi"}},
	 {"id":"g","name":"History","type":"@n8n/n8n-nodes-langchain.memoryManager","typeVersion":1.1,"position":[400,0],"parameters":{}},
	 {"id":"mem","name":"Memory","type":"@n8n/n8n-nodes-langchain.memoryBufferWindow","typeVersion":1.3,"position":[300,200],
	  "parameters":{"sessionIdType":"customKey","sessionKey":"support"}}],
	 "connections":{"Start":{"main":[[{"node":"Agent","type":"main","index":0}]]},
	  "Agent":{"main":[[{"node":"History","type":"main","index":0}]]},
	  "Memory":{"ai_memory":[[{"node":"Agent","type":"ai_memory","index":0},{"node":"History","type":"ai_memory","index":0}]]}}}`
	wf, rep, err := n8nimport.Convert([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	a, h := attachedSub(t, wf, "Agent", "ai_memory"), attachedSub(t, wf, "History", "ai_memory")
	if a.ID != h.ID || a.Type != "ai.memory" {
		t.Errorf("agent memory %s (%s), manager memory %s", a.ID, a.Type, h.ID)
	}
	wantConfig(t, a, map[string]interface{}{"session_key": "support", "window": 10})
	if _, set := node(t, wf, "History").Config["session_key"]; set {
		t.Error("session_key set on the memory manager")
	}
	n := 0
	for _, x := range wf.Nodes {
		if x.Type == "ai.memory" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d ai.memory nodes", n)
	}
	validateClean(t, wf, rep)
}

// fakeOpenAIEmbeddings is an OpenAI-compatible embeddings API: a text gets
// a vector by its first letter.
func fakeOpenAIEmbeddings(t *testing.T, calls *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/embeddings" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "text-embedding-3-small" {
			t.Errorf("model %q", body.Model)
		}
		*calls++
		var data []map[string]interface{}
		for i, s := range body.Input {
			vec := []float64{0, 1}
			if strings.HasPrefix(strings.ToLower(s), "a") {
				vec = []float64{1, 0}
			}
			data = append(data, map[string]interface{}{"index": i, "embedding": vec})
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func ragWorkflow(baseURL, key string) string {
	emb := func(name string, x int) string {
		return `{"name":"` + name + `","type":"@n8n/n8n-nodes-langchain.embeddingsOpenAi","typeVersion":1.2,"position":[` + fmt.Sprint(x) + `,200],
		  "parameters":{"model":"text-embedding-3-small","options":{"baseURL":"` + baseURL + `"}}}`
	}
	return `{"name":"rag","nodes":[
	 {"name":"Start","type":"n8n-nodes-base.manualTrigger","typeVersion":1,"position":[0,0],"parameters":{}},
	 {"name":"Docs","type":"n8n-nodes-base.set","typeVersion":3.4,"position":[100,0],"parameters":{"assignments":{"assignments":[
	   {"id":"1","name":"body","value":"apples are red\nbolts are steel","type":"string"}]}}},
	 {"name":"Insert","type":"@n8n/n8n-nodes-langchain.vectorStoreInMemory","typeVersion":1.3,"position":[200,0],
	  "parameters":{"mode":"insert","memoryKey":{"__rl":true,"mode":"list","value":"` + key + `"}}},
	 ` + emb("Embed Insert", 200) + `,
	 {"name":"Loader","type":"@n8n/n8n-nodes-langchain.documentDefaultDataLoader","typeVersion":1.1,"position":[300,200],
	  "parameters":{"jsonMode":"expressionData","jsonData":"={{ $json.body }}","textSplittingMode":"custom"}},
	 {"name":"Splitter","type":"@n8n/n8n-nodes-langchain.textSplitterCharacterTextSplitter","typeVersion":1,"position":[300,400],
	  "parameters":{"separator":"\n","chunkSize":20,"chunkOverlap":0}},
	 {"name":"Ask","type":"n8n-nodes-base.set","typeVersion":3.4,"position":[400,0],"executeOnce":true,"parameters":{"assignments":{"assignments":[
	   {"id":"1","name":"question","value":"bolts?","type":"string"}]}}},
	 {"name":"Search","type":"@n8n/n8n-nodes-langchain.vectorStoreInMemory","typeVersion":1.3,"position":[600,0],
	  "parameters":{"mode":"load","memoryKey":{"__rl":true,"mode":"list","value":"` + key + `"},"prompt":"={{ $json.question }}","topK":1}},
	 ` + emb("Embed Search", 600) + `
	],"connections":{
	 "Start":{"main":[[{"node":"Docs","type":"main","index":0}]]},
	 "Docs":{"main":[[{"node":"Insert","type":"main","index":0}]]},
	 "Insert":{"main":[[{"node":"Ask","type":"main","index":0}]]},
	 "Ask":{"main":[[{"node":"Search","type":"main","index":0}]]},
	 "Embed Insert":{"ai_embedding":[[{"node":"Insert","type":"ai_embedding","index":0}]]},
	 "Loader":{"ai_document":[[{"node":"Insert","type":"ai_document","index":0}]]},
	 "Splitter":{"ai_textSplitter":[[{"node":"Loader","type":"ai_textSplitter","index":0}]]},
	 "Embed Search":{"ai_embedding":[[{"node":"Search","type":"ai_embedding","index":0}]]}
	}}`
}

// A vector store inserting through a document loader with a text splitter,
// and one retrieving, both with embeddings: imported 1:1 as sub-nodes, the
// result validates and runs on the engine — the stores use their attached
// embeddings, the insert loads and splits its items with the attached
// loader and splitter, and no sub-node runs as a step.
func TestVectorSubNodesImportValidateAndRun(t *testing.T) {
	calls := 0
	srv := fakeOpenAIEmbeddings(t, &calls)
	wf, rep, err := n8nimport.Convert([]byte(ragWorkflow(srv.URL, t.Name())))
	if err != nil {
		t.Fatal(err)
	}
	insert := node(t, wf, "Insert")
	wantConfig(t, insert, map[string]interface{}{"provider": "memory", "mode": "insert", "memory_key": t.Name()})
	wantConfig(t, attachedSub(t, wf, "Insert", "ai_embedding"), map[string]interface{}{"provider": "openai_compatible",
		"model": "text-embedding-3-small", "base_url": srv.URL})
	loader := attachedSub(t, wf, "Insert", "ai_document")
	wantConfig(t, loader, map[string]interface{}{"text": "{{ $json.body }}"})
	wantConfig(t, attachedSub(t, wf, "Loader", "ai_textSplitter"), map[string]interface{}{"splitter": "character",
		"separator": "\n", "chunk_size": 20, "chunk_overlap": 0})
	if attachedSub(t, wf, "Search", "ai_embedding").Name != "Embed Search" {
		t.Error("search embeddings")
	}
	parsed := validateClean(t, wf, rep)

	db := testdb.Open(t)
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db.DB), "p1")
	store := workflow.NewSQLiteWorkflowStore(db.DB)
	parsed.ProfileID = "p1"
	if err := store.CreateWorkflow(ctx, &parsed); err != nil {
		t.Fatal(err)
	}
	dag, err := workflow.BuildDAG(parsed.Nodes, parsed.Connections)
	if err != nil {
		t.Fatal(err)
	}
	e := &workflow.WorkflowExecution{WorkflowID: parsed.ID, ProfileID: "p1", TriggerType: "manual", TriggerData: map[string]interface{}{}}
	if err := store.CreateExecution(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := workflow.RunExecution(ctx, e, &parsed, dag, noderegistry.Build(db.DB), store, nil, workflow.NewExpressionEngine(), zerolog.Nop()); err != nil {
		t.Fatalf("run: %v", err)
	}
	rows, err := store.GetExecutionNodeOutputs(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]string{}
	for _, n := range parsed.Nodes {
		byName[n.ID] = n.Name
	}
	outputs := map[string]string{}
	for _, r := range rows {
		name := byName[r.NodeID]
		if strings.HasPrefix(name, "Embed") || name == "Loader" || name == "Splitter" {
			t.Errorf("sub-node %s ran as a step", name)
		}
		outputs[name] = r.OutputRaw
	}
	if !strings.Contains(outputs["Insert"], `"pageContent":"apples are red"`) || !strings.Contains(outputs["Insert"], `"pageContent":"bolts are steel"`) {
		t.Errorf("insert output %s", outputs["Insert"])
	}
	if !strings.Contains(outputs["Search"], `"pageContent":"bolts are steel"`) || strings.Contains(outputs["Search"], "apples") {
		t.Errorf("search output %s", outputs["Search"])
	}
	if calls != 2 {
		t.Errorf("%d embedding requests, want 2 (one insert batch, one query)", calls)
	}
}

// A Question and Answer Chain's vector-store retriever keeps the store's
// embeddings as a sub-node of the search step; the result validates.
func TestRetrieverEmbeddingsAttachToSearch(t *testing.T) {
	src := `{"name":"qa","nodes":[
	 {"name":"Start","type":"n8n-nodes-base.manualTrigger","typeVersion":1,"position":[0,0],"parameters":{}},
	 {"name":"QA","type":"@n8n/n8n-nodes-langchain.chainRetrievalQa","typeVersion":1.5,"position":[200,0],"parameters":{}},
	 {"name":"Model","type":"@n8n/n8n-nodes-langchain.lmChatAnthropic","typeVersion":1.3,"position":[100,200],
	  "parameters":{"model":{"__rl":true,"mode":"list","value":"claude-sonnet-4-5"}}},
	 {"name":"Retriever","type":"@n8n/n8n-nodes-langchain.retrieverVectorStore","typeVersion":1,"position":[300,200],"parameters":{"topK":2}},
	 {"name":"Store","type":"@n8n/n8n-nodes-langchain.vectorStoreInMemory","typeVersion":1.3,"position":[300,400],"parameters":{"memoryKey":"kb"}},
	 {"name":"Ollama","type":"@n8n/n8n-nodes-langchain.embeddingsOllama","typeVersion":1,"position":[300,600],"parameters":{"model":"nomic-embed-text"}}],
	 "connections":{"Start":{"main":[[{"node":"QA","type":"main","index":0}]]},
	  "Model":{"ai_languageModel":[[{"node":"QA","type":"ai_languageModel","index":0}]]},
	  "Retriever":{"ai_retriever":[[{"node":"QA","type":"ai_retriever","index":0}]]},
	  "Store":{"ai_vectorStore":[[{"node":"Retriever","type":"ai_vectorStore","index":0}]]},
	  "Ollama":{"ai_embedding":[[{"node":"Store","type":"ai_embedding","index":0}]]}}}`
	wf, rep, err := n8nimport.Convert([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	search := node(t, wf, "QA (retrieve)")
	wantConfig(t, search, map[string]interface{}{"provider": "memory", "mode": "search", "memory_key": "kb", "top_k": 2})
	emb := attachedSub(t, wf, "QA (retrieve)", "ai_embedding")
	if emb.Type != "vector.embeddings" || emb.Name != "Ollama" {
		t.Errorf("embeddings %s %q", emb.Type, emb.Name)
	}
	wantConfig(t, emb, map[string]interface{}{"provider": "ollama", "model": "nomic-embed-text"})
	wantConfig(t, attachedSub(t, wf, "QA (answer)", "ai_languageModel"), map[string]interface{}{"runtime": "claude", "model": "claude-sonnet-4-5"})
	for _, name := range []string{"Retriever", "Store"} {
		if nr := nodeReport(t, rep, name); nr.Status != n8nimport.StatusFolded {
			t.Errorf("%s status %s", name, nr.Status)
		}
	}
	validateClean(t, wf, rep)
}
