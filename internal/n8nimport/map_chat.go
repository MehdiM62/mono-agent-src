package n8nimport

// n8n's Chat Trigger becomes trigger.chat (a chat page at /chat/<path>,
// each message a run with {sessionId, action, chatInput}, as n8n's), and
// its Chat node ("Respond to Chat") core.respond_chat.

func init() {
	Register("@n8n/n8n-nodes-langchain.chat", mapRespondChat)
}

func mapChatTrigger(c *MapContext) Mapping {
	path := webhookPath(c)
	cfg := map[string]interface{}{"path": path}
	if c.Literal("mode", "hostedChat") == "webhook" {
		c.Warn("mode", "webhook", "the embedded chat's page is not imported; post {sessionId, chatInput} to /chat/%s/message from your page, or use the hosted page at /chat/%s", path, path)
	}
	if !c.Bool("public", false) {
		c.Note("n8n only chatted with this workflow from its editor; mono-agent serves the chat at /chat/%s while the workflow is active", path)
	}
	switch auth := c.Literal("authentication", "none"); auth {
	case "none":
	default:
		c.Warn("authentication", auth, "trigger.chat has no sign-in; the chat at /chat/%s is open to anyone who can reach the webhook server", path)
	}
	if v := c.Str("initialMessages", ""); v != "" {
		cfg["initialMessages"] = v
	}
	for n8nKey, key := range map[string]string{"options.title": "title", "options.subtitle": "subtitle", "options.inputPlaceholder": "inputPlaceholder"} {
		if v := c.Str(n8nKey, ""); v != "" {
			cfg[key] = v
		}
	}
	switch mode := c.Literal("options.responseMode", "lastNode"); mode {
	case "lastNode", "responseNodes":
		cfg["responseMode"] = mode
	case "streaming":
		cfg["responseMode"] = "lastNode"
		c.Warn("options.responseMode", mode, "answers are not streamed; the chat shows the last node's output when the run finishes")
	default:
		cfg["responseMode"] = "lastNode"
		c.Warn("options.responseMode", mode, "the chat shows the last node's output (its output or text field) when the run finishes; a Respond to Webhook answer is not shown")
	}
	if c.Bool("options.allowFileUploads", false) {
		c.Warn("options.allowFileUploads", true, "the chat page takes no file uploads")
	}
	c.Ignore("options.allowedFilesMimeTypes")
	if v := c.Literal("options.loadPreviousSession", "notSupported"); v != "notSupported" {
		c.Warn("options.loadPreviousSession", v, "the chat page shows the session's messages kept by the daemon (a day after the last one), not a memory's history")
	}
	if c.Has("options.customCss") {
		c.Warn("options.customCss", "(custom CSS)", "the chat page uses mono-agent's own style; custom CSS is not applied")
	}
	if c.Has("options.allowedOrigins") {
		c.Warn("options.allowedOrigins", c.Raw("options.allowedOrigins"), "cross-origin pages are allowed through MONOAGENT_WEBHOOK_ALLOWED_ORIGINS instead")
	}
	c.Ignore("options.showWelcomeScreen", "options.getStarted", "options.includeUserInOutput", "includeUserInOutput",
		"availableInChat", "agentName", "agentDescription", "agentIcon", "suggestedPrompts", "requireExecuteAccess",
		"hostedChatNotice", "embeddedChatNotice", "availableInChatNotice", "options.autoSaveHighlightedData")
	c.Note("the chat is served at /chat/%s on the webhook server while the workflow is active", path)
	return Mapping{Type: "trigger.chat", Config: cfg}
}

// mapRespondChat maps the Chat node: version 1 waited for the reply unless
// waitUserReply was off; from 1.1 the operation says so. Version 1.1 nested
// a typed answer under data.
func mapRespondChat(c *MapContext) Mapping {
	cfg := map[string]interface{}{"message": c.Str("message", "")}
	op := "send"
	if c.version(1.1) {
		op = c.Literal("operation", "send")
	} else if c.Bool("waitUserReply", true) {
		op = "sendAndWait"
	}
	switch op {
	case "send", "sendAndWait":
	default:
		c.Warn("operation", op, "is not send or sendAndWait; the node sends the message")
		op = "send"
	}
	cfg["operation"] = op
	if op == "sendAndWait" {
		if rt := c.Literal("responseType", "freeTextChat"); rt == "approval" {
			cfg["responseType"] = "approval"
			if c.Has("approvalOptions") {
				cfg["approvalOptions"] = c.Value("approvalOptions")
			}
			if c.Bool("blockUserInput", false) {
				cfg["blockUserInput"] = true
			}
		} else {
			cfg["responseType"] = "freeTextChat"
		}
		if c.Has("options.limitWaitTime") {
			cfg["options"] = map[string]interface{}{"limitWaitTime": c.Value("options.limitWaitTime")}
		}
		if c.TypeVersion == 1.1 {
			cfg["nestUnderData"] = true
		}
	} else {
		c.Ignore("responseType", "approvalOptions", "blockUserInput", "options.limitWaitTime")
	}
	if c.Bool("options.memoryConnection", false) {
		c.Warn("options.memoryConnection", true, "the chat's messages are not added to a memory; the AI agent's own memory keeps its side of the conversation")
	}
	c.Ignore("generalNotice", "options.autoSaveHighlightedData")
	return Mapping{Type: "core.respond_chat", Config: cfg}
}
