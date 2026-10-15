package httpnodes

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func gqlServer(t *testing.T, reply string) *recorder {
	return newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	})
}

func TestGraphQLPostJSON(t *testing.T) {
	rec := gqlServer(t, `{"data":{"user":{"name":"Ann"}}}`)
	m := mustOK(t, &GraphQLNode{}, map[string]interface{}{
		"endpoint":       rec.URL + "/graphql",
		"query":          "query U($login: String!) { user(login: $login) { name } }",
		"variables":      `{"login": "{{ $json.login }}"}`,
		"operation_name": "U",
		"headers":        map[string]interface{}{"X-Trace": "1"},
	}, workflow.NewItem(map[string]interface{}{"login": "ann"}))
	got := rec.last(t)
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(got.Body), &doc); err != nil {
		t.Fatal(err)
	}
	if got.Method != "POST" || got.Header.Get("Content-Type") != "application/json" || got.Header.Get("X-Trace") != "1" {
		t.Fatalf("request: %s %v", got.Method, got.Header)
	}
	if doc["operationName"] != "U" || doc["variables"].(map[string]interface{})["login"] != "ann" || !strings.HasPrefix(doc["query"].(string), "query U") {
		t.Fatalf("document = %v", doc)
	}
	if m[0].JSON["data"].(map[string]interface{})["user"].(map[string]interface{})["name"] != "Ann" {
		t.Fatalf("item = %v", m[0].JSON)
	}
}

func TestGraphQLOperationNameNullWhenEmpty(t *testing.T) {
	rec := gqlServer(t, `{"data":{}}`)
	mustOK(t, &GraphQLNode{}, map[string]interface{}{"endpoint": rec.URL, "query": "{ a }"})
	body := rec.last(t).Body
	if !strings.Contains(body, `"operationName":null`) || !strings.Contains(body, `"variables":{}`) {
		t.Fatalf("body = %s", body)
	}
}

func TestGraphQLGetAndRawFormat(t *testing.T) {
	rec := gqlServer(t, `{"data":{"a":1}}`)
	mustOK(t, &GraphQLNode{}, map[string]interface{}{
		"endpoint": rec.URL + "?v=2", "query": "{ a }", "request_method": "GET",
		"variables": map[string]interface{}{"x": 1.0}, "operation_name": "Q",
	})
	got := rec.last(t)
	if got.Method != "GET" || got.Query["query"][0] != "{ a }" || got.Query["variables"][0] != `{"x":1}` || got.Query["operationName"][0] != "Q" || got.Query["v"][0] != "2" || got.Body != "" {
		t.Fatalf("GET request: %+v", got)
	}

	mustOK(t, &GraphQLNode{}, map[string]interface{}{"endpoint": rec.URL, "query": "{ b }", "request_format": "graphql"})
	got = rec.last(t)
	if got.Header.Get("Content-Type") != "application/graphql" || got.Body != "{ b }" {
		t.Fatalf("raw request: %q %q", got.Header.Get("Content-Type"), got.Body)
	}
}

func TestGraphQLErrorsGoToErrorHandle(t *testing.T) {
	rec := gqlServer(t, `{"data":{"a":null},"errors":[{"message":"no access"},{"message":"bad field"}]}`)
	m, e := execute(t, &GraphQLNode{}, map[string]interface{}{"endpoint": rec.URL, "query": "{ a }"})
	if len(m) != 0 || len(e) != 1 || e[0].JSON["error"] != "no access, bad field" || e[0].JSON["data"] == nil {
		t.Fatalf("got %v %v", m, e)
	}
}

func TestGraphQLHTTPErrorAndInvalidJSON(t *testing.T) {
	srv := serve(t, 502, "text/plain", "bad gateway", nil)
	_, e := execute(t, &GraphQLNode{}, map[string]interface{}{"endpoint": srv, "query": "{ a }"})
	if len(e) != 1 || !strings.Contains(e[0].JSON["error"].(string), "not valid JSON") || e[0].JSON["status"] != 502 {
		t.Fatalf("got %v", e)
	}
	m, e := execute(t, &GraphQLNode{}, map[string]interface{}{"endpoint": serve(t, 200, "text/plain", "ok", nil), "query": "{ a }", "response_format": "string", "data_property_name": "raw"})
	if len(e) != 0 || m[0].JSON["raw"] != "ok" {
		t.Fatalf("text format: %v %v", m, e)
	}
}

func TestGraphQLConfigErrors(t *testing.T) {
	for _, tc := range []struct {
		cfg  map[string]interface{}
		want string
	}{
		{map[string]interface{}{"query": "{ a }"}, "endpoint"},
		{map[string]interface{}{"endpoint": "http://x"}, "query"},
		{map[string]interface{}{"endpoint": "http://x", "query": "{ a }", "request_method": "PUT"}, "request_method"},
	} {
		if _, err := (&GraphQLNode{}).Execute(t.Context(), workflow.NodeInput{}, tc.cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err = %v", tc.cfg, err)
		}
	}
	_, e := execute(t, &GraphQLNode{}, map[string]interface{}{"endpoint": "http://127.0.0.1:1", "query": "{ a }", "variables": "[1]"})
	if len(e) != 1 || !strings.Contains(e[0].JSON["error"].(string), "variables") {
		t.Fatalf("bad variables: %v", e)
	}
}

func TestGraphQLBasicAuth(t *testing.T) {
	rec := gqlServer(t, `{"data":{}}`)
	mustOK(t, &GraphQLNode{}, map[string]interface{}{"endpoint": rec.URL, "query": "{ a }", "auth_type": "basic", "auth_username": "u", "auth_password": "p"})
	if rec.last(t).Header.Get("Authorization") != basicHeader("u", "p") {
		t.Fatal("basic auth not sent")
	}
}
