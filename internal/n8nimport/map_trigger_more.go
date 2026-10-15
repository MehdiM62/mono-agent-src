package n8nimport

import (
	"fmt"
	"strings"
)

// Core n8n triggers beyond schedules and webhooks: the n8n Form
// (map_form.go), the SSE trigger, the MCP server trigger, and the trigger
// types that import as placeholders (triggerPlaceholders). The evaluation
// dataset trigger is in map_evaluation.go.

func init() {
	Register("n8n-nodes-base.formTrigger", mapFormTrigger)
	Register("n8n-nodes-base.sseTrigger", mapSSETrigger)
	Register("n8n-nodes-base.n8nTrigger", mapWorkflowTrigger)
	Register("@n8n/n8n-nodes-langchain.mcpTrigger", mapMCPTrigger)
	for t, reason := range triggerPlaceholders {
		reason := reason
		Register(t, func(c *MapContext) Mapping {
			for k := range c.Params {
				c.Ignore(k)
			}
			c.issues = append(c.issues, Issue{Severity: SevError,
				Message: fmt.Sprintf("n8n trigger %s cannot be imported: %s; start the workflow with a schedule or webhook trigger instead", c.Type, reason)})
			return Mapping{Unmapped: true}
		})
	}
}

// triggerPlaceholders are the n8n trigger types with no runnable mono
// construct, with the reason the report gives.
var triggerPlaceholders = map[string]string{
	"n8n-nodes-base.e2eTestPollingTrigger": "it is n8n's internal end-to-end test node",
	"@n8n/n8n-nodes-langchain.microsoftAgent365Trigger": "Microsoft Agent 365 activities (Bot Framework requests with token " +
		"validation, answered inside the conversation) have no mono-agent equivalent",
}

// TriggerPlaceholder returns why an n8n trigger type imports as a
// placeholder, or "" when it imports to a runnable trigger.
func TriggerPlaceholder(n8nType string) string { return triggerPlaceholders[n8nType] }

// mapSSETrigger: trigger.sse keeps the stream open and starts a run per
// event as {event, data, id}; n8n passed on only unnamed ("message")
// events, as their parsed JSON data, so a code step does the same. Data
// that is not a JSON object arrives as {data}. Credentials in the URL
// (user:pass@host) are sent as Basic auth, as n8n did.
func mapSSETrigger(c *MapContext) Mapping {
	u := c.Literal("url", "")
	if u == "" {
		c.issues = append(c.issues, Issue{Severity: SevError, Param: "url", Message: "the SSE Trigger has no URL"})
	}
	body := `return items.filter(function (it) { return it.json.event === "message"; }).map(function (it) {
  var d = it.json.data;
  return d !== null && typeof d === "object" && !Array.isArray(d) ? d : { data: d };
});
`
	return Mapping{Type: "trigger.sse", Config: map[string]interface{}{"url": u}, EntrySuffix: " (stream)",
		Chain: []ChainStep{{Type: "core.code", Config: codeConfig(wrapCode(body))}}}
}

// mapMCPTrigger: n8n served the trigger's tool sub-nodes as an MCP server.
// mono-agent's own MCP server (monoagentcli mcp) runs saved workflows
// through its workflow_run tool, so the workflow starts manually and each
// tool becomes a workflow of its own.
func mapMCPTrigger(c *MapContext) Mapping {
	c.Ignore("path", "authentication", "options")
	var tools []string
	for _, s := range c.SubNodes {
		tools = append(tools, s.Name)
	}
	msg := "n8n served this trigger's tools as an MCP server; mono-agent's MCP server (monoagentcli mcp) runs saved workflows through its workflow_run tool"
	if len(tools) > 0 {
		msg += fmt.Sprintf(": rebuild each tool (%s) as a workflow and call it from your MCP client", strings.Join(tools, ", "))
	}
	c.Warn("tools", nil, "%s", msg)
	return Mapping{Type: "trigger.manual", Config: map[string]interface{}{}}
}
