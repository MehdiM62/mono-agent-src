package n8nimport_test

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/n8nimport"
)

// n8n's generic OAuth1 credential (HTTP Request 4.x genericCredentialType
// and 1.x authentication: oAuth1) becomes a generic_oauth1 connection the
// node signs with.
func TestHTTPRequestOAuth1ImportsAsGenericOAuth1Connection(t *testing.T) {
	wf, rep, err := n8nimport.Convert([]byte(`{"name":"t","nodes":[
	 {"name":"M","type":"n8n-nodes-base.manualTrigger","typeVersion":1,"position":[0,0],"parameters":{}},
	 {"name":"V4","type":"n8n-nodes-base.httpRequest","typeVersion":4.2,"position":[200,0],
	  "parameters":{"url":"https://api.example.com/1.1/me","authentication":"genericCredentialType","genericAuthType":"oAuth1Api"},
	  "credentials":{"oAuth1Api":{"id":"7","name":"Example OAuth1"}}},
	 {"name":"V1","type":"n8n-nodes-base.httpRequest","typeVersion":1,"position":[400,0],
	  "parameters":{"url":"https://api.example.com/1.1/me","authentication":"oAuth1"},
	  "credentials":{"oAuth1Api":{"id":"7","name":"Example OAuth1"}}}],
	 "connections":{"M":{"main":[[{"node":"V4","type":"main","index":0}]]},"V4":{"main":[[{"node":"V1","type":"main","index":0}]]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"V4", "V1"} {
		wantConfig(t, node(t, wf, name), map[string]interface{}{"auth_type": "credential", "credential_id": "generic_oauth1"})
		if _, ok := node(t, wf, name).Config["auth_token"]; ok {
			t.Errorf("%s: OAuth1 still imported as a bearer token", name)
		}
	}
	if len(rep.Credentials) != 1 || rep.Credentials[0].Platform != "generic_oauth1" ||
		!strings.Contains(rep.Credentials[0].Hint, "connect oauth generic_oauth1") || len(rep.Credentials[0].Nodes) != 2 {
		t.Errorf("credentials = %+v", rep.Credentials)
	}
}
