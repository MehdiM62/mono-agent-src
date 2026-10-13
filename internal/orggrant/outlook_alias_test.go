package orggrant

import (
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// comm.outlook_read / comm.outlook_send are aliases of service.outlook_mail;
// only the legacy read shape (no operation, no send keys) is read-only.
func TestOutlookMailClassifiedByConfig(t *testing.T) {
	cases := []struct {
		name     string
		typ      string
		config   map[string]interface{}
		outbound bool
	}{
		{"legacy read", "comm.outlook_read", map[string]interface{}{"mailbox": "INBOX", "limit": 10}, false},
		{"legacy read shape on canonical type", "service.outlook_mail", map[string]interface{}{"mailbox": "INBOX"}, false},
		{"legacy read type with an operation", "comm.outlook_read", map[string]interface{}{"operation": "send_message"}, true},
		{"legacy send", "comm.outlook_send", map[string]interface{}{"to": "a@x", "subject": "s", "body": "b"}, true},
		{"legacy send with empty to", "comm.outlook_send", map[string]interface{}{"to": ""}, true},
		{"canonical list_messages stays outbound", "service.outlook_mail", map[string]interface{}{"operation": "list_messages"}, true},
		{"canonical send", "service.outlook_mail", map[string]interface{}{"operation": "send_message"}, true},
	}
	for _, tc := range cases {
		got := isOutboundNode(workflow.WorkflowNode{Type: tc.typ, Config: tc.config})
		if got != tc.outbound {
			t.Errorf("%s: outbound = %v, want %v", tc.name, got, tc.outbound)
		}
	}
	if fileNodeTypes["service.outlook_mail"] == nil {
		t.Error("service.outlook_mail attachments are not declared as a file input")
	}
}
