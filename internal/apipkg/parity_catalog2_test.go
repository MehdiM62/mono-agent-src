package apipkg

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// seenReq is one request a test server received, body included.
type seenReq struct {
	Method, Path, Query, Type, Body string
	Header                          http.Header
}

// recServer answers each request with fn and records it.
func recServer(t *testing.T, fn func(r seenReq) string) *[]seenReq {
	t.Helper()
	var reqs []seenReq
	pagedServer(t, func(_ int, r *http.Request, w http.ResponseWriter) {
		b, _ := io.ReadAll(r.Body)
		s := seenReq{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Type: r.Header.Get("Content-Type"), Body: string(b), Header: r.Header.Clone()}
		reqs = append(reqs, s)
		fmt.Fprint(w, fn(s))
	})
	return &reqs
}

func runItems(t *testing.T, p *Package, cfg map[string]any, items ...workflow.Item) ([]workflow.Item, error) {
	t.Helper()
	out, err := run(context.Background(), p, workflow.NodeInput{Items: items}, cfg, true)
	if err != nil {
		return nil, err
	}
	return out[0].Items, nil
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Dropbox lists and searches page through /continue endpoints that take
// only the cursor; the first request still sends path "".
func TestDropboxContinuePaging(t *testing.T) {
	p := catalogPkg(t, "dropbox")
	for _, c := range []struct{ res, op, first, cont, key string }{
		{"folder", "list", "/2/files/list_folder", "/2/files/list_folder/continue", "entries"},
		{"search", "query", "/2/files/search_v2", "/2/files/search/continue_v2", "matches"},
	} {
		reqs := recServer(t, func(r seenReq) string {
			switch r.Path {
			case "/2/users/get_current_account":
				return `{"root_info":{"root_namespace_id":"77"}}`
			case c.first:
				return `{"` + c.key + `":[{"name":"a"}],"cursor":"c1","has_more":true}`
			}
			return `{"` + c.key + `":[{"name":"b"}],"cursor":"c2","has_more":false}`
		})
		cfg := map[string]any{"resource": c.res, "operation": c.op, "returnAll": true, "query": "q", "simple": false,
			"credential": map[string]any{"access_token": "t"}}
		items, err := runItems(t, p, cfg)
		if err != nil {
			t.Fatalf("%s: %v", c.op, err)
		}
		if len(items) != 2 || len(*reqs) != 3 {
			t.Fatalf("%s: %d items, requests %v", c.op, len(items), *reqs)
		}
		cont := (*reqs)[2]
		if cont.Path != c.cont || cont.Body != `{"cursor":"c1"}` || !strings.Contains(cont.Header.Get("Dropbox-API-Path-Root"), `"77"`) {
			t.Errorf("%s continue request %+v", c.op, cont)
		}
		if c.op == "list" && !strings.Contains((*reqs)[1].Body, `"path":""`) {
			t.Errorf("list first body %s", (*reqs)[1].Body)
		}
	}
	// A limit stops after the first page that reaches it.
	reqs := recServer(t, func(r seenReq) string {
		return `{"entries":[{"name":"a"},{"name":"b"}],"cursor":"c1","has_more":true}`
	})
	items, err := runItems(t, p, map[string]any{"resource": "folder", "operation": "list", "limit": 2,
		"credential": map[string]any{"access_token": "t", "access_type": "app"}})
	if err != nil || len(items) != 2 || len(*reqs) != 1 {
		t.Errorf("limit: %d items, %d requests, %v", len(items), len(*reqs), err)
	}
}

// SeaTable drops an image the row already holds instead of listing it twice.
func TestSeaTableUploadDedupes(t *testing.T) {
	p := catalogPkg(t, "sea_table")
	url := "https://cloud.seatable.io/workspace/7/asset/U1/images/x/a.png"
	var update string
	recServer(t, func(r seenReq) string {
		switch r.Path {
		case "/api/v2.1/dtable/app-upload-link/":
			return `{"upload_link":"https://cloud.seatable.io/seafhttp/upload-api/TOK","parent_path":"/asset/U1","img_relative_path":"images/x","file_relative_path":"files/x"}`
		case "/api/v2.1/dtable/app-access-token/":
			return `{"access_token":"AT","dtable_uuid":"U1","workspace_id":7}`
		case "/api-gateway/api/v2/dtables/U1/rows/r1":
			return `{"Col":["old","` + url + `"]}`
		case "/seafhttp/upload-api/TOK":
			return `[{"name":"a.png","size":2}]`
		}
		update = r.Body
		return `{"success":true}`
	})
	cfg := map[string]any{"resource": "asset", "operation": "upload", "tableName": "T", "uploadColumn": "Col:::image",
		"rowId": "r1", "credential": map[string]any{"token": "API"}}
	if _, err := runItems(t, p, cfg, workflow.Item{JSON: map[string]any{}, Binary: map[string][]byte{"data": []byte("hi")}}); err != nil {
		t.Fatal(err)
	}
	if want := `"row":{"Col":["old","` + url + `"]}`; !strings.Contains(update, want) {
		t.Errorf("update %s, want %s", update, want)
	}
}

// Excel upsert with updateAll rewrites every matching row of the range;
// when none matches it grows the range by one row holding the new values
// (the match column included).
func TestExcelUpsertUpdateAll(t *testing.T) {
	sheet := `{"address":"Sheet1!A1:C4","values":[["name","age","city"],["ada","36","x"],["bob","40","y"],["ada","1","z"]]}`
	for _, id := range []string{"microsoft_excel", "microsoft_excel_share_point"} {
		p := catalogPkg(t, id)
		for _, c := range []struct{ value, rng, values string }{
			{"ada", "A1:C4", `[["name","age","city"],[null,"9",null],["bob","40","y"],[null,"9",null]]`},
			{"zed", "A1:C5", `[["name","age","city"],["ada","36","x"],["bob","40","y"],["ada","1","z"],["zed","9",null]]`},
		} {
			var patch seenReq
			recServer(t, func(r seenReq) string {
				if r.Method == http.MethodGet {
					return sheet
				}
				patch = r
				return `{"values":[["name","age","city"],["ada","9","x"],["bob","40","y"],["ada","9","z"],["zed","9",""]]}`
			})
			cfg := map[string]any{"resource": "worksheet", "operation": "upsert", "workbook": "W1", "worksheet": "Sheet1",
				"site": "S1", "library": "D1",
				"columnToMatchOn": "name", "valueToMatchOn": c.value, "options": map[string]any{"updateAll": true},
				"fieldsUi":   map[string]any{"values": []any{map[string]any{"column": "age", "fieldValue": "9"}}},
				"credential": graphCred}
			items, err := runItems(t, p, cfg)
			if err != nil {
				t.Fatalf("%s %s: %v", id, c.value, err)
			}
			if !strings.Contains(patch.Path, "range(address='"+c.rng+"')") {
				t.Errorf("%s %s: path %s", id, c.value, patch.Path)
			}
			var body struct{ Values any }
			json.Unmarshal([]byte(patch.Body), &body)
			if got := jsonOf(body.Values); got != c.values {
				t.Errorf("%s %s: values %s, want %s", id, c.value, got, c.values)
			}
			want := 2
			if c.value == "zed" {
				want = 1
			}
			if len(items) != want {
				t.Errorf("%s %s: %d output rows %v", id, c.value, len(items), items)
			}
		}
	}
}

// Elasticsearch bulkOperation sends the items as one NDJSON _bulk request
// and returns one item per bulk result; an item error fails the node.
func TestElasticsearchBulk(t *testing.T) {
	p := catalogPkg(t, "elasticsearch")
	es := map[string]any{"username": "u", "password": "p", "base_url": "https://es.test"}
	resp := `{"errors":false,"items":[{"index":{"_id":"1","result":"created"}},{"index":{"_id":"x","result":"created"}}]}`
	reqs := recServer(t, func(seenReq) string { return resp })
	cfg := map[string]any{"resource": "document", "operation": "create", "indexId": "{{ $json.idx }}",
		"dataToSend": "autoMapInputData", "inputsToIgnore": "idx", "options": map[string]any{"bulkOperation": true},
		"additionalFields": map[string]any{"documentId": "{{ $json.id }}"}, "credential": es}
	items, err := runItems(t, p, cfg,
		workflow.Item{JSON: map[string]any{"idx": "a", "id": "1", "v": 1.0}},
		workflow.Item{JSON: map[string]any{"idx": "b", "v": 2.0}})
	if err != nil {
		t.Fatal(err)
	}
	if len(*reqs) != 1 {
		t.Fatalf("requests %v", *reqs)
	}
	r := (*reqs)[0]
	wantBody := `{"index":{"_id":"1","_index":"a"}}` + "\n" + `{"id":"1","v":1}` + "\n" + `{"index":{"_index":"b"}}` + "\n" + `{"v":2}` + "\n"
	if r.Method != "POST" || r.Path != "/_bulk" || r.Type != "application/x-ndjson" || r.Body != wantBody {
		t.Errorf("bulk request %s %s %s %q", r.Method, r.Path, r.Type, r.Body)
	}
	if len(items) != 2 || items[0].JSON["result"] != "created" {
		t.Errorf("items %v", items)
	}

	// Delete and update lines; size 50 splits 51 items into two requests.
	reqs = recServer(t, func(seenReq) string { return `{"errors":false,"items":[{"delete":{"_id":"1"}}]}` })
	var many []workflow.Item
	for i := range 51 {
		many = append(many, workflow.Item{JSON: map[string]any{"id": fmt.Sprint(i)}})
	}
	cfg = map[string]any{"resource": "document", "operation": "delete", "indexId": "i", "documentId": "{{ $json.id }}",
		"options": map[string]any{"bulkOperation": true}, "credential": es}
	if _, err := runItems(t, p, cfg, many...); err != nil {
		t.Fatal(err)
	}
	if len(*reqs) != 2 || strings.Count((*reqs)[0].Body, "\n") != 50 || (*reqs)[1].Body != `{"delete":{"_id":"50","_index":"i"}}`+"\n" {
		t.Errorf("delete chunks: %d requests", len(*reqs))
	}
	recServer(t, func(seenReq) string {
		return `{"errors":true,"items":[{"update":{"_id":"1","error":{"type":"document_missing_exception","reason":"[1]: document missing"}}}]}`
	})
	cfg = map[string]any{"resource": "document", "operation": "update", "indexId": "i", "documentId": "1",
		"fieldsUi": map[string]any{"fieldValues": []any{map[string]any{"fieldId": "a", "fieldValue": "b"}}},
		"options":  map[string]any{"bulkOperation": true}, "credential": es}
	if _, err := runItems(t, p, cfg, workflow.Item{JSON: map[string]any{}}); err == nil || !strings.Contains(err.Error(), "document missing") {
		t.Errorf("bulk item error: %v", err)
	}
}

// Elasticsearch getAll with Return all and a sort opens a point in time,
// pages with search_after (each page's pit_id), and closes the PIT; $1
// placeholders in the query take the query parameters.
func TestElasticsearchPointInTimePaging(t *testing.T) {
	p := catalogPkg(t, "elasticsearch")
	reqs := recServer(t, func(r seenReq) string {
		switch {
		case r.Path == "/logs/_pit":
			return `{"id":"P0"}`
		case r.Method == "DELETE":
			return `{"succeeded":true}`
		case strings.Contains(r.Body, `"search_after":[2,"b"]`):
			return `{"pit_id":"P2","hits":{"hits":[]}}`
		case strings.Contains(r.Body, "search_after"):
			return `{"pit_id":"P2","hits":{"hits":[{"_id":"b","_source":{"n":2},"sort":[2,"b"]}]}}`
		}
		return `{"pit_id":"P1","hits":{"hits":[{"_id":"a","_source":{"n":1},"sort":[1,"a"]}]}}`
	})
	cfg := map[string]any{"resource": "document", "operation": "getAll", "indexId": "logs", "returnAll": true,
		"options":    map[string]any{"sort": "n:asc", "query": `{"query":{"match":{"$1":"$2"}}}`, "queryParameters": `["msg","boom"]`},
		"credential": map[string]any{"username": "u", "password": "p", "base_url": "https://es.test"}}
	items, err := runItems(t, p, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := jsonOf([]any{items[0].JSON, items[1].JSON}); len(items) != 2 || got != `[{"_id":"a","n":1},{"_id":"b","n":2}]` {
		t.Fatalf("items %v", items)
	}
	var lines []string
	for _, r := range *reqs {
		lines = append(lines, r.Method+" "+r.Path+" "+r.Body)
	}
	want := []string{
		"POST /logs/_pit ",
		`POST /_search {"pit":{"id":"P0","keep_alive":"1m"},"query":{"match":{"msg":"boom"}},"size":10000,"track_total_hits":false}`,
		`POST /_search {"pit":{"id":"P1","keep_alive":"1m"},"query":{"match":{"msg":"boom"}},"search_after":[1,"a"],"size":10000,"track_total_hits":false}`,
		`POST /_search {"pit":{"id":"P2","keep_alive":"1m"},"query":{"match":{"msg":"boom"}},"search_after":[2,"b"],"size":10000,"track_total_hits":false}`,
		`DELETE /_pit {"id":"P2"}`,
	}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if q, _ := url.ParseQuery((*reqs)[0].Query); q.Get("keep_alive") != "1m" {
		t.Errorf("pit query %s", (*reqs)[0].Query)
	}
	if q, _ := url.ParseQuery((*reqs)[2].Query); q.Get("sort") != "n:asc" || q.Get("size") != "10000" {
		t.Errorf("continuation query %s", (*reqs)[2].Query)
	}
}
