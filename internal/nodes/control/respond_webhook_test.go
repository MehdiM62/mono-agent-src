package control

import (
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func respondItems() []workflow.Item {
	return []workflow.Item{
		{JSON: map[string]interface{}{"id": float64(1)}},
		{JSON: map[string]interface{}{"id": float64(2)}},
	}
}

func TestBuildWebhookResponse(t *testing.T) {
	cases := []struct {
		name     string
		config   map[string]interface{}
		code     int
		body     string
		ctype    string
		header   string
		headerTo string
	}{
		{"default first item", map[string]interface{}{}, 200, `{"id":1}`, "application/json", "", ""},
		{"all items", map[string]interface{}{"respond_with": "allIncomingItems"}, 200, `[{"id":1},{"id":2}]`, "application/json", "", ""},
		{"json text", map[string]interface{}{"respond_with": "json", "response_body": `{"ok":true}`, "response_code": float64(201)}, 201, `{"ok":true}`, "application/json", "", ""},
		{"json object", map[string]interface{}{"respond_with": "json", "response_body": map[string]interface{}{"ok": "yes"}}, 200, `{"ok":"yes"}`, "application/json", "", ""},
		{"text", map[string]interface{}{"respond_with": "text", "response_body": "hi", "response_code": "202"}, 202, "hi", "text/plain; charset=utf-8", "", ""},
		{"no data", map[string]interface{}{"respond_with": "noData", "response_code": float64(204)}, 204, "", "", "", ""},
		{"headers", map[string]interface{}{"respond_with": "text", "response_body": "x", "response_headers": `{"X-Id":"7","Content-Type":"text/csv"}`}, 200, "x", "text/csv", "X-Id", "7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := buildWebhookResponse(respondItems(), tc.config)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tc.code || string(resp.Body) != tc.body || resp.Headers["Content-Type"] != tc.ctype {
				t.Errorf("got %d %q %q, want %d %q %q", resp.StatusCode, resp.Body, resp.Headers["Content-Type"], tc.code, tc.body, tc.ctype)
			}
			if tc.header != "" && resp.Headers[tc.header] != tc.headerTo {
				t.Errorf("header %s = %q, want %q", tc.header, resp.Headers[tc.header], tc.headerTo)
			}
		})
	}
}

func TestBuildWebhookResponse_InvalidConfig(t *testing.T) {
	for _, cfg := range []map[string]interface{}{
		{"respond_with": "xml"},
		{"respond_with": "json", "response_body": "{not json"},
		{"response_code": float64(700)},
		{"response_headers": "[1,2]"},
	} {
		if _, err := buildWebhookResponse(respondItems(), cfg); !errors.Is(err, workflow.ErrInvalidConfig) {
			t.Errorf("%v: err = %v, want ErrInvalidConfig", cfg, err)
		}
	}
}

// The node passes its items through whether or not a request is waiting.
func TestRespondWebhookNode_PassesItemsThrough(t *testing.T) {
	out, err := (&RespondWebhookNode{}).Execute(t.Context(), workflow.NodeInput{Items: respondItems(), ExecutionID: "nobody-waiting"}, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Handle != "main" || len(out[0].Items) != 2 {
		t.Fatalf("outputs = %+v, want the 2 items on main", out)
	}
}
