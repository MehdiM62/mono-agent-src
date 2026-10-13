package recording

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPICallsKeepsJSONAndDropsRepeats(t *testing.T) {
	entries := []NetEntry{
		{Method: "GET", URL: "https://a.test/api/items", ContentType: "application/json; charset=utf-8", Status: 200},
		{Method: "GET", URL: "https://a.test/style.css", ContentType: "text/css", Status: 200},
		{Method: "GET", URL: "https://a.test/api/items", ContentType: "application/json", Status: 200},
		{Method: "POST", URL: "https://a.test/api/items", ContentType: "application/problem+json", Status: 422, RequestBody: `{"a":1}`},
	}
	calls := APICalls(entries)
	if len(calls) != 2 || calls[0].N != 1 || calls[1].N != 2 || calls[1].Method != "POST" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestBuildHTTPNodeUsesRequestNodeKeys(t *testing.T) {
	c := APICall{N: 1, NetEntry: NetEntry{
		Method: "POST", URL: "https://api.example.com/v1/items", ContentType: "application/json",
		RequestType: "application/json", RequestBody: `{"title":"hi"}`,
		RequestHeaders: []NetHeader{
			{Name: "Authorization", Credential: true, Scheme: "Bearer"},
			{Name: "X-Api-Key", Credential: true},
			{Name: "Accept", Value: "application/json"},
			{Name: "Content-Type", Value: "application/json"},
		},
	}}
	n := BuildHTTPNode(c)
	cfg := n.Config
	if cfg["method"] != "POST" || cfg["url"] != "https://api.example.com/v1/items" || cfg["response_format"] != "json" {
		t.Errorf("cfg = %v", cfg)
	}
	if cfg["body_type"] != "json" || cfg["body"] != `{"title":"hi"}` {
		t.Errorf("body = %v / %v", cfg["body_type"], cfg["body"])
	}
	if cfg["auth_type"] != "bearer" || cfg["auth_token"] != "@secret:api-example-com-authorization" {
		t.Errorf("auth = %v / %v", cfg["auth_type"], cfg["auth_token"])
	}
	headers := cfg["headers"].(map[string]any)
	if headers["X-Api-Key"] != "@secret:api-example-com-x-api-key" || headers["Accept"] != "application/json" {
		t.Errorf("headers = %v", headers)
	}
	if _, ok := headers["Content-Type"]; ok {
		t.Errorf("content-type should come from body_type: %v", headers)
	}
	if strings.Join(n.Secrets, ",") != "api-example-com-authorization,api-example-com-x-api-key" {
		t.Errorf("secrets = %v", n.Secrets)
	}
}

func TestBuildHTTPNodeNonBearerAuthAndForm(t *testing.T) {
	c := APICall{N: 1, NetEntry: NetEntry{Method: "PUT", URL: "https://b.test/x", RequestType: "application/x-www-form-urlencoded",
		RequestBody:    "a=1",
		RequestHeaders: []NetHeader{{Name: "Authorization", Credential: true, Scheme: "Basic"}}}}
	cfg := BuildHTTPNode(c).Config
	if _, ok := cfg["auth_type"]; ok {
		t.Errorf("basic auth should be a whole-header secret: %v", cfg)
	}
	if cfg["headers"].(map[string]any)["Authorization"] != "@secret:b-test-authorization" || cfg["body_type"] != "form" {
		t.Errorf("cfg = %v", cfg)
	}
}

func TestBuildAPIWorkflowChainsNodes(t *testing.T) {
	calls := []APICall{
		{N: 1, NetEntry: NetEntry{Method: "GET", URL: "https://a.test/api/me", RequestHeaders: []NetHeader{{Name: "Authorization", Credential: true, Scheme: "Bearer"}}}},
		{N: 2, NetEntry: NetEntry{Method: "POST", URL: "https://a.test/api/items", RequestBody: `{"secret":"` + Redacted + `"}`,
			RequestHeaders: []NetHeader{{Name: "Authorization", Credential: true, Scheme: "Bearer"}}}},
	}
	wf, secrets := BuildAPIWorkflow("From recording", calls)
	if len(wf.Nodes) != 3 || wf.Nodes[0].Type != "trigger.manual" || wf.Nodes[1].Type != "http.request" || wf.Nodes[2].ID != "call_2" {
		t.Fatalf("nodes = %+v", wf.Nodes)
	}
	if len(wf.Connections) != 2 || wf.Connections[0].Source != "start" || wf.Connections[1].Target != "call_2" {
		t.Errorf("connections = %+v", wf.Connections)
	}
	if len(secrets) != 1 || secrets[0] != "a-test-authorization" {
		t.Errorf("secrets = %v", secrets)
	}
	for _, want := range []string{"a-test-authorization", Redacted, "cookies were not recorded"} {
		if !strings.Contains(wf.Description, want) {
			t.Errorf("description lacks %q: %s", want, wf.Description)
		}
	}
	if _, err := json.Marshal(wf); err != nil {
		t.Fatal(err)
	}
}

func TestLoadNetworkMissingIsEmpty(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadNetwork(dir)
	if err != nil || got != nil {
		t.Fatalf("got %v, %v", got, err)
	}
	os.WriteFile(filepath.Join(dir, NetworkArtifact), []byte(`{"method":"GET","url":"https://a.test/x","status":200}`+"\nnot json\n"), 0o600)
	got, err = LoadNetwork(dir)
	if err != nil || len(got) != 1 || got[0].URL != "https://a.test/x" {
		t.Fatalf("got %v, %v", got, err)
	}
}
