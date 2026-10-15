package httpnodes

import (
	"encoding/json"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func schemaKeys(t *testing.T, nodeType string) map[string]bool {
	t.Helper()
	raw, ok := workflow.ReadEmbeddedSchema(nodeType)
	if !ok {
		t.Fatalf("no embedded schema for %s", nodeType)
	}
	var s struct {
		Fields []struct {
			Key string `json:"key"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, f := range s.Fields {
		keys[f.Key] = true
	}
	return keys
}

// Every config key the nodes read is in their schema, and every n8n
// mapping targets a key the schema shows.
func TestSchemasCoverConfigKeys(t *testing.T) {
	req := schemaKeys(t, "http.request")
	for _, k := range []string{
		"credential_id", "credential_auth", "url", "method", "auth_type", "auth_username", "auth_password", "auth_token",
		"auth_api_key_in", "auth_api_key_name", "auth_api_key_value", "query_params", "query_array_format", "headers",
		"body_type", "body", "raw_content_type", "binary_property", "response_format", "full_response", "never_error",
		"output_property", "lowercase_headers", "follow_redirects", "max_redirects", "send_credentials_on_redirect",
		"allow_unauthorized_certs", "proxy", "timeout", "pagination_type", "page_field", "page_size",
		"pagination_parameters", "pagination_next_url", "pagination_complete_when", "pagination_status_codes",
		"pagination_complete_expression", "max_pages", "pagination_interval_ms", "batch_size", "batch_interval_ms",
		"propagate_trace", "max_body_mb",
	} {
		if !req[k] {
			t.Errorf("http.request schema lacks %s", k)
		}
	}
	for n8n, p := range N8nHTTPParamMap {
		if p.Key != "" && !req[p.Key] {
			t.Errorf("N8nHTTPParamMap[%s] -> %s is not an http.request field", n8n, p.Key)
		}
	}
	gql := schemaKeys(t, "http.graphql")
	for n8n, p := range N8nGraphQLParamMap {
		if p.Key != "" && !gql[p.Key] {
			t.Errorf("N8nGraphQLParamMap[%s] -> %s is not an http.graphql field", n8n, p.Key)
		}
	}
}

// The n8n option values the maps translate are accepted by the node as is.
func TestN8nValuesAreAccepted(t *testing.T) {
	for v, ours := range N8nHTTPParamMap["contentType"].Values {
		if bodyTypeAliases[v] != ours && v != ours {
			t.Errorf("contentType %s is not accepted as body_type", v)
		}
	}
	for v, ours := range N8nHTTPParamMap["options.pagination.pagination.paginationMode"].Values {
		if paginationModeAliases[v] != ours && v != ours {
			t.Errorf("paginationMode %s is not accepted", v)
		}
	}
	for v, ours := range N8nHTTPParamMap["genericAuthType"].Values {
		if ours != "" && authTypeAliases[v] != ours {
			t.Errorf("genericAuthType %s is not accepted as auth_type", v)
		}
	}
}
