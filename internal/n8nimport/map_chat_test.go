package n8nimport_test

import "testing"

// The Chat Trigger imports as trigger.chat: its page at /chat/<webhookId>
// starts runs with n8n's item, answered by the last node or Chat nodes.
func TestChatTriggerImportsAsChat(t *testing.T) {
	wf, rep := convertNode(t, "@n8n/n8n-nodes-langchain.chatTrigger", 1.3, map[string]interface{}{
		"public": true, "initialMessages": "Hi!\nAsk away.", "authentication": "basicAuth",
		"options": map[string]interface{}{"responseMode": "responseNodes", "title": "Help", "inputPlaceholder": "Ask…", "customCss": "x{}"},
	})
	n := node(t, wf, "Trigger")
	if n.Type != "trigger.chat" {
		t.Fatalf("type %s", n.Type)
	}
	wantConfig(t, n, map[string]interface{}{"path": "hook-1", "initialMessages": "Hi!\nAsk away.", "title": "Help",
		"inputPlaceholder": "Ask…", "responseMode": "responseNodes"})
	wantIssue(t, rep, "no sign-in")
	wantIssue(t, rep, "custom CSS is not applied")

	wf, rep = convertNode(t, "@n8n/n8n-nodes-langchain.chatTrigger", 1.3, map[string]interface{}{
		"options": map[string]interface{}{"responseMode": "streaming"}})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"responseMode": "lastNode"})
	wantIssue(t, rep, "not streamed")
}

func TestChatNodeImportsAsRespondChat(t *testing.T) {
	wf, _ := convertNode(t, "@n8n/n8n-nodes-langchain.chat", 1.3, map[string]interface{}{
		"operation": "sendAndWait", "message": "Book it?", "responseType": "approval", "blockUserInput": true,
		"approvalOptions": map[string]interface{}{"values": map[string]interface{}{"approvalType": "double"}},
		"options":         map[string]interface{}{"limitWaitTime": map[string]interface{}{"values": map[string]interface{}{"resumeAmount": 5.0, "resumeUnit": "minutes"}}},
	})
	n := node(t, wf, "Trigger")
	if n.Type != "core.respond_chat" {
		t.Fatalf("type %s", n.Type)
	}
	wantConfig(t, n, map[string]interface{}{"operation": "sendAndWait", "message": "Book it?", "responseType": "approval",
		"blockUserInput": true, "approvalOptions": map[string]interface{}{"values": map[string]interface{}{"approvalType": "double"}},
		"options": map[string]interface{}{"limitWaitTime": map[string]interface{}{"values": map[string]interface{}{"resumeAmount": 5.0, "resumeUnit": "minutes"}}}})

	// Version 1 waited for the reply by default; 1.1 nested the answer under data.
	wf, _ = convertNode(t, "@n8n/n8n-nodes-langchain.chat", 1, map[string]interface{}{"message": "Name?"})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"operation": "sendAndWait", "responseType": "freeTextChat"})
	wf, _ = convertNode(t, "@n8n/n8n-nodes-langchain.chat", 1.1, map[string]interface{}{"message": "Name?", "operation": "sendAndWait"})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"nestUnderData": true})
	wf, _ = convertNode(t, "@n8n/n8n-nodes-langchain.chat", 1.3, map[string]interface{}{"message": "Done."})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"operation": "send", "message": "Done."})
}
