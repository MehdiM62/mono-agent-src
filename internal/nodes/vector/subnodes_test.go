package vector

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// fakeEmbedder is an OpenAI-compatible embeddings API: texts starting with
// "a" point one way, the rest the other.
func fakeEmbedder(t *testing.T, calls *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/embeddings" || r.Header.Get("Authorization") != "Bearer sub-key" {
			t.Errorf("request %s %s auth %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		raw, _ := io.ReadAll(r.Body)
		var b struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.Unmarshal(raw, &b); err != nil {
			t.Errorf("body %s: %v", raw, err)
		}
		*calls = append(*calls, b.Model+":"+strings.Join(b.Input, "|"))
		var data []map[string]interface{}
		for i, s := range b.Input {
			vec := []float64{0, 1}
			if strings.HasPrefix(s, "a") {
				vec = []float64{1, 0}
			}
			data = append(data, map[string]interface{}{"index": i, "embedding": vec})
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func embeddingsSub(url string) workflow.SubNode {
	return workflow.SubNode{ID: "emb", Name: "Embeddings", Type: TypeEmbeddings, Kind: KindEmbedding,
		Config: map[string]interface{}{"provider": "openai_compatible", "base_url": url, "api_key": "sub-key", "model": "sub-model"}}
}

func storeRun(t *testing.T, cfg map[string]interface{}, subs []workflow.SubNode, items ...workflow.Item) ([]workflow.Item, error) {
	t.Helper()
	out, err := (&StoreNode{}).Execute(context.Background(), workflow.NodeInput{Items: items, SubNodes: subs}, cfg)
	if err != nil {
		return nil, err
	}
	return out[0].Items, nil
}

func TestStoreUsesAttachedEmbeddings(t *testing.T) {
	var calls []string
	srv := fakeEmbedder(t, &calls)
	subs := []workflow.SubNode{embeddingsSub(srv.URL)}
	// The attached sub-node wins over an inline block.
	inline := map[string]interface{}{"provider": "cohere", "api_key": "inline", "base_url": "http://127.0.0.1:1"}
	cfg := map[string]interface{}{"provider": "memory", "memory_key": t.Name(), "mode": "insert", "embedding": inline}
	if _, err := storeRun(t, cfg, subs, workflow.NewItem(map[string]interface{}{"text": "apple"}),
		workflow.NewItem(map[string]interface{}{"text": "bolt"})); err != nil {
		t.Fatal(err)
	}
	hits, err := storeRun(t, map[string]interface{}{"provider": "memory", "memory_key": t.Name(), "mode": "search", "top_k": 1.0},
		subs, workflow.NewItem(map[string]interface{}{"chatInput": "bolts please"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].JSON["pageContent"] != "bolt" {
		t.Fatalf("hits %v", hits)
	}
	if strings.Join(calls, ",") != "sub-model:apple|bolt,sub-model:bolts please" {
		t.Fatalf("embedding calls %v", calls)
	}
	bad := embeddingsSub(srv.URL)
	bad.Config = map[string]interface{}{"provider": "cohere"}
	_, err = storeRun(t, cfg, []workflow.SubNode{bad}, workflow.NewItem(map[string]interface{}{"text": "x"}))
	if err == nil || !strings.Contains(err.Error(), `vector.store: embeddings "Embeddings": api_key is required for provider cohere`) {
		t.Fatalf("bad sub-node: %v", err)
	}
}

func TestStoreInsertsAttachedLoaderDocuments(t *testing.T) {
	var calls []string
	srv := fakeEmbedder(t, &calls)
	splitter := workflow.SubNode{ID: "sp", Name: "Splitter", Type: TypeTextSplitter, Kind: KindTextSplitter,
		Config: map[string]interface{}{"splitter": "character", "separator": "\\n", "chunk_size": 10.0, "chunk_overlap": 0.0}}
	loader := workflow.SubNode{ID: "ld", Name: "Loader", Type: TypeDocumentLoader, Kind: KindDocument,
		Config:   map[string]interface{}{"source": "json", "text": "{{ $json.body }}", "metadata_fields": "topic"},
		SubNodes: []workflow.SubNode{splitter}}
	cfg := map[string]interface{}{"provider": "memory", "memory_key": t.Name(), "mode": "insert", "content_field": "body"}
	out, err := storeRun(t, cfg, []workflow.SubNode{embeddingsSub(srv.URL), loader},
		workflow.NewItem(map[string]interface{}{"body": "apples\nbolts", "topic": "a"}),
		workflow.NewItem(map[string]interface{}{"body": "anchors", "topic": "b"}))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range out {
		meta, _ := it.JSON["metadata"].(map[string]interface{})
		got = append(got, it.JSON["pageContent"].(string)+"/"+meta["topic"].(string))
	}
	if strings.Join(got, ",") != "apples/a,bolts/a,anchors/b" {
		t.Fatalf("inserted %v", got)
	}
	if strings.Join(calls, ",") != "sub-model:apples|bolts|anchors" {
		t.Fatalf("embedding calls %v", calls)
	}
	// Search and delete leave the loader out.
	hits, err := storeRun(t, map[string]interface{}{"provider": "memory", "memory_key": t.Name(), "mode": "search",
		"query": "bolt", "top_k": 1.0}, []workflow.SubNode{embeddingsSub(srv.URL), loader})
	if err != nil || len(hits) != 1 || hits[0].JSON["pageContent"] != "bolts" {
		t.Fatalf("search %v %v", hits, err)
	}
	loader.Config = map[string]interface{}{"source": "binary"}
	_, err = storeRun(t, cfg, []workflow.SubNode{embeddingsSub(srv.URL), loader}, workflow.NewItem(map[string]interface{}{}))
	if err == nil || !strings.Contains(err.Error(), `document loader "Loader": vector.load_document: item 0: item has no binary "data"`) {
		t.Fatalf("loader error: %v", err)
	}
}

func TestLoadDocumentUsesAttachedSplitter(t *testing.T) {
	splitter := workflow.SubNode{Name: "Splitter", Type: TypeTextSplitter, Kind: KindTextSplitter,
		Config: map[string]interface{}{"splitter": "character", "separator": " ", "chunk_size": 3.0}}
	// The inline split settings give way to the attached splitter.
	cfg := map[string]interface{}{"source": "json", "split": false, "chunk_size": 1000.0, "separator": "x"}
	out, err := (&LoadDocumentNode{}).Execute(context.Background(), workflow.NodeInput{
		Items: []workflow.Item{workflow.NewItem(map[string]interface{}{"text": "ab cd"})}, SubNodes: []workflow.SubNode{splitter}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if items := out[0].Items; len(items) != 2 || items[0].JSON["pageContent"] != "ab" || items[1].JSON["pageContent"] != "cd" {
		t.Fatalf("chunks %v", items)
	}
	if cfg["split"] != false || cfg["chunk_size"] != 1000.0 {
		t.Fatalf("config changed: %v", cfg)
	}
}

func TestVectorSubNodesFailWhenRunAsStep(t *testing.T) {
	r := workflow.NewNodeTypeRegistry()
	RegisterAll(r)
	for typ, want := range map[string]string{
		TypeEmbeddings:     "vector.embeddings is a sub-node: connect its ai_embedding output to a Vector Store",
		TypeDocumentLoader: "vector.document_loader is a sub-node: connect its ai_document output to a Vector Store",
		TypeTextSplitter:   "vector.text_splitter is a sub-node: connect its ai_textSplitter output to a Document Loader",
	} {
		f, ok := r.Get(typ)
		if !ok {
			t.Fatalf("%s not registered", typ)
		}
		_, err := f().Execute(context.Background(), workflow.NodeInput{}, nil)
		if !errors.Is(err, workflow.ErrInvalidConfig) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v", typ, err)
		}
	}
	f, _ := r.Get(TypeDocumentLoader)
	if res, ok := f().(workflow.PerItemConfigResolver); !ok || strings.Join(res.PerItemConfigFields(), ",") != "text" {
		t.Error("the document loader's text is not held back per item")
	}
}
