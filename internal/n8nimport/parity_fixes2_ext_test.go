package n8nimport_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/n8nimport"
)

func noteText(rep *n8nimport.Report) string {
	b, _ := json.Marshal(rep)
	return string(b)
}

// A Wufoo post is shaped only when it carries the handshake key the
// import made (and reported); others are dropped, as n8n dropped them.
func TestWufooHandshakeKeyDropsForeignPosts(t *testing.T) {
	defer n8nimport.WithHandshakeKey("s3cret")()
	wf, rep := convertNodes(t, `{"name":"W","type":"n8n-nodes-base.wufooTrigger","typeVersion":1,"position":[0,0],"webhookId":"wf","parameters":{"form":"f1"}}`)
	if !strings.Contains(noteText(rep), "s3cret") {
		t.Errorf("the report does not name the handshake key: %s", noteText(rep))
	}
	fields := `{"Fields":[{"ID":"Field1","Title":"Name","Type":"text"}]}`
	got := runNode(t, wf, "W", []map[string]interface{}{
		{"body": map[string]interface{}{"Field1": "Ada", "FieldStructure": fields, "HandshakeKey": "s3cret"}},
		{"body": map[string]interface{}{"Field1": "Eve", "FieldStructure": fields, "HandshakeKey": "guess"}},
		{"body": map[string]interface{}{"Field1": "Bob", "FieldStructure": fields}},
	})
	expectJSON(t, "wufoo", got, `[{"Name":"Ada"}]`)
}

// Notion nodes from before n8n version 3 fold property keys to ASCII: the
// package's "<3" version default reaches the imported config.
func TestNotionImportKeyCaseByVersion(t *testing.T) {
	for ver, want := range map[string]any{"2.2": "ascii", "1": "ascii", "3": nil} {
		wf, _ := convertNodes(t, `{"name":"N","type":"n8n-nodes-base.notion","typeVersion":`+ver+`,"position":[0,0],
		  "parameters":{"resource":"page","operation":"search","text":"x"},"credentials":{"notionApi":{"id":"1","name":"Notion"}}}`)
		var cfg map[string]interface{}
		for _, n := range wf.Nodes {
			if n.Name == "N" {
				cfg = n.Config
			}
		}
		if cfg == nil || cfg["operation"] != "search" || cfg["keyCase"] != want {
			t.Errorf("v%s: config %v", ver, cfg)
		}
	}
}

// n8n Twitter v1 tweet attachments name binary properties; v2 holds media
// IDs. The package's "<2" version default tells them apart on import.
func TestTwitterImportAttachmentsByVersion(t *testing.T) {
	for ver, want := range map[string]any{"1": "binaryProperties", "2": nil} {
		wf, _ := convertNodes(t, `{"name":"X","type":"n8n-nodes-base.twitter","typeVersion":`+ver+`,"position":[0,0],
		  "parameters":{"text":"hi","additionalFields":{"attachments":"data"}},"credentials":{"twitterOAuth1Api":{"id":"1","name":"X"}}}`)
		var cfg map[string]interface{}
		for _, n := range wf.Nodes {
			if n.Name == "X" {
				cfg = n.Config
			}
		}
		if cfg == nil || cfg["attachmentsAre"] != want {
			t.Errorf("v%s: config %v", ver, cfg)
		}
	}
}
