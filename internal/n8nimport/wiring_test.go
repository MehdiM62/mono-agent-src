package n8nimport_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/n8nimport"
	_ "github.com/monoes/mono-agent/internal/noderegistry" // registers the API package app source
	"github.com/monoes/mono-agent/internal/workflow"
)

func convertFile(t *testing.T, name string) (*workflow.WorkflowFile, *n8nimport.Report) {
	t.Helper()
	raw, err := os.ReadFile("testdata/n8n/" + name)
	if err != nil {
		t.Fatal(err)
	}
	wf, rep, err := n8nimport.Convert(raw)
	if err != nil {
		t.Fatalf("convert %s: %v", name, err)
	}
	return wf, rep
}

func node(t *testing.T, wf *workflow.WorkflowFile, name string) workflow.WorkflowFileNode {
	t.Helper()
	for _, n := range wf.Nodes {
		if n.Name == name {
			return n
		}
	}
	t.Fatalf("no node %q", name)
	return workflow.WorkflowFileNode{}
}

func wantConfig(t *testing.T, n workflow.WorkflowFileNode, want map[string]interface{}) {
	t.Helper()
	for k, v := range want {
		if got := n.Config[k]; !equalJSON(got, v) {
			t.Errorf("%s: config[%s] = %#v, want %#v", n.Name, k, got, v)
		}
	}
}

func equalJSON(a, b interface{}) bool {
	return strings.TrimSpace(jsonString(a)) == strings.TrimSpace(jsonString(b))
}

// An n8n Todoist node imports as the todoist API package's node: n8n's
// resource default filled in, parameters intact, the resource locator
// unwrapped, expressions translated, and the credential hint naming the
// field n8n calls differently.
func TestTodoistImportsAsAppNode(t *testing.T) {
	wf, rep := convertFile(t, "todoist_app.json")
	create := node(t, wf, "Create task")
	if create.Type != "app.todoist" {
		t.Fatalf("type = %s", create.Type)
	}
	wantConfig(t, create, map[string]interface{}{
		"resource": "task", "operation": "create", "project": "2203306141", "labels": []interface{}{"home"},
		"content": "{{ $json.title }}", "credential_id": "todoist",
		"options": map[string]interface{}{"priority": 4.0, "dueString": "tomorrow"},
	})
	wantConfig(t, node(t, wf, "Close task"), map[string]interface{}{
		"resource": "task", "operation": "close", "taskId": "{{ $json.id }}"})
	if len(rep.Credentials) != 1 || !strings.Contains(rep.Credentials[0].Hint, "apiKey → access_token") {
		t.Errorf("credentials: %+v", rep.Credentials)
	}
	_, app := n8nimport.SupportedTypes()
	if !contains(app, "n8n-nodes-base.todoist") {
		t.Errorf("SupportedTypes lacks the todoist package type")
	}
}

func TestSplitOutAndSummarizeUseDataNodes(t *testing.T) {
	wf, rep := convertFile(t, "split_out_summarize.json")
	split := node(t, wf, "Split Out")
	if split.Type != "data.split_out" {
		t.Fatalf("type = %s", split.Type)
	}
	wantConfig(t, split, map[string]interface{}{"field_to_split_out": "lines", "include": "all_other_fields", "destination_field_name": "line"})
	sum := node(t, wf, "Summarize")
	wantConfig(t, sum, map[string]interface{}{"fields_to_split_by": "order", "output_format": "single_item",
		"fields_to_summarize": []interface{}{
			map[string]interface{}{"aggregation": "sum", "field": "line.qty"},
			map[string]interface{}{"aggregation": "concatenate", "field": "customer", "separate_by": "other", "custom_separator": " | "},
		}})
	if rep.Counts.Full != 3 {
		t.Errorf("report: %s", rep.Text())
	}
}

// A vector store's embedding sub-node and document loader (with its text
// splitter) are kept as sub-nodes attached through n8n's connection types.
func TestVectorStoreSubNodesAttach(t *testing.T) {
	wf, rep := convertFile(t, "vector_rag.json")
	store := node(t, wf, "Qdrant Vector Store")
	wantConfig(t, store, map[string]interface{}{"provider": "qdrant", "mode": "insert", "collection": "docs", "credential_id": "qdrant"})
	if _, set := store.Config["embedding"]; set {
		t.Errorf("embedding folded into the store: %v", store.Config["embedding"])
	}
	emb := attachedSub(t, wf, "Qdrant Vector Store", "ai_embedding")
	if emb.Type != "vector.embeddings" || emb.Name != "Embeddings OpenAI" {
		t.Errorf("embeddings %s %q", emb.Type, emb.Name)
	}
	wantConfig(t, emb, map[string]interface{}{"provider": "openai_compatible", "model": "text-embedding-3-small", "credential_id": "openai"})
	loader := attachedSub(t, wf, "Qdrant Vector Store", "ai_document")
	if loader.Type != "vector.document_loader" || loader.Name != "Default Data Loader" {
		t.Errorf("loader %s %q", loader.Type, loader.Name)
	}
	wantConfig(t, loader, map[string]interface{}{"source": "json"})
	splitter := attachedSub(t, wf, "Default Data Loader", "ai_textSplitter")
	if splitter.Type != "vector.text_splitter" {
		t.Errorf("splitter %s", splitter.Type)
	}
	wantConfig(t, splitter, map[string]interface{}{"splitter": "recursive", "chunk_size": 500.0, "chunk_overlap": 50.0})
	search := node(t, wf, "Search")
	wantConfig(t, search, map[string]interface{}{"mode": "search", "top_k": 3.0, "query": "{{ $json.question }}"})
	if e := attachedSub(t, wf, "Search", "ai_embedding"); e.Name != "Embeddings OpenAI 2" {
		t.Errorf("search embeddings %q", e.Name)
	}
	if len(wf.Nodes) != 7 {
		t.Errorf("every n8n node is a mono node: %d nodes", len(wf.Nodes))
	}
	main := map[string]string{}
	for _, c := range wf.Connections {
		if c.SourceHandle == "main" {
			main[c.Source] = c.Target
		}
	}
	if len(main) != 2 || main["Manual"] != store.ID || main[store.ID] != search.ID {
		t.Errorf("main connections: %+v", wf.Connections)
	}
	for _, name := range []string{"Embeddings OpenAI", "Default Data Loader", "Recursive Splitter", "Embeddings OpenAI 2"} {
		if nr := nodeReport(t, rep, name); nr.Status != n8nimport.StatusFull || !strings.HasPrefix(nr.MonoType, "vector.") {
			t.Errorf("%s: status %s type %s", name, nr.Status, nr.MonoType)
		}
	}
	platforms := []string{}
	for _, c := range rep.Credentials {
		platforms = append(platforms, c.Platform)
	}
	if strings.Join(platforms, ",") != "openai,qdrant" {
		t.Errorf("credentials: %v", platforms)
	}
	validates(t, wf)
}

// HTTP Request 4.x uses http.request's n8n options; n8n's defaults that
// differ from ours are written out.
func TestHTTPRequestUsesParamMap(t *testing.T) {
	wf, _ := convertFile(t, "http_graphql.json")
	wantConfig(t, node(t, wf, "List users"), map[string]interface{}{
		"auth_type": "credential", "credential_id": "github", "timeout": 15.0, "proxy": "http://proxy.local:3128",
		"batch_size": 5.0, "batch_interval_ms": 200.0, "pagination_type": "next_url", "pagination_complete_when": "expression",
		"max_pages": 20.0, "never_error": true, "full_response": false, "response_format": "autodetect", "lowercase_headers": true,
		"query_params": []interface{}{
			map[string]interface{}{"name": "per_page", "value": "50"},
			map[string]interface{}{"name": "tag", "value": "a"},
			map[string]interface{}{"name": "tag", "value": "b"},
		},
	})
	up := node(t, wf, "Upload")
	wantConfig(t, up, map[string]interface{}{"method": "POST", "body_type": "multipart", "full_response": true,
		"response_format": "json", "lowercase_headers": false, "auth_type": "credential", "credential_id": "generic_api"})
	if rows, _ := up.Config["body"].([]interface{}); len(rows) != 2 {
		t.Errorf("multipart rows: %v", up.Config["body"])
	}
	gql := node(t, wf, "GraphQL")
	if gql.Type != "http.graphql" {
		t.Fatalf("type = %s", gql.Type)
	}
	wantConfig(t, gql, map[string]interface{}{"endpoint": "https://api.example.com/graphql", "request_method": "post",
		"request_format": "json", "response_format": "json", "variables": map[string]interface{}{"id": "1"}})
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func jsonString(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Markdown's default mode (HTML to Markdown) and Edit Image import through
// the data and image parameter tables.
func TestMarkdownAndEditImageUseParamTables(t *testing.T) {
	wf, rep, err := n8nimport.Convert([]byte(`{"name":"t","nodes":[
	 {"name":"M","type":"n8n-nodes-base.manualTrigger","typeVersion":1,"position":[0,0],"parameters":{}},
	 {"name":"To Markdown","type":"n8n-nodes-base.markdown","typeVersion":1,"position":[200,0],
	  "parameters":{"html":"={{ $json.body }}","options":{"bulletMarker":"*"}}},
	 {"name":"Resize","type":"n8n-nodes-base.editImage","typeVersion":1,"position":[400,0],
	  "parameters":{"operation":"resize","width":200,"height":100,"options":{"format":"png"}}}],
	 "connections":{"M":{"main":[[{"node":"To Markdown","type":"main","index":0}]]},
	  "To Markdown":{"main":[[{"node":"Resize","type":"main","index":0}]]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	md := node(t, wf, "To Markdown")
	if md.Type != "data.markdown" {
		t.Fatalf("type = %s", md.Type)
	}
	wantConfig(t, md, map[string]interface{}{"mode": "html_to_markdown", "html": "{{ $json.body }}", "bullet_marker": "*"})
	img := node(t, wf, "Resize")
	wantConfig(t, img, map[string]interface{}{"operation": "resize", "width": 200.0, "height": 100.0, "format": "png", "hex_alpha": "opacity"})
	if img.Type != "image.edit" || !rep.Complete {
		t.Errorf("type %s; report: %s", img.Type, rep.Text())
	}
}

// attachedSub returns the sub-node attached to the node named root through
// a kind connection (source and target handle both kind).
func attachedSub(t *testing.T, wf *workflow.WorkflowFile, root, kind string) workflow.WorkflowFileNode {
	t.Helper()
	r := node(t, wf, root)
	for _, c := range wf.Connections {
		if c.Target == r.ID && c.TargetHandle == kind {
			if c.SourceHandle != kind {
				t.Errorf("%s connection into %s leaves handle %q", kind, root, c.SourceHandle)
			}
			for _, n := range wf.Nodes {
				if n.ID == c.Source {
					return n
				}
			}
		}
	}
	t.Fatalf("no %s sub-node attached to %q", kind, root)
	return workflow.WorkflowFileNode{}
}
