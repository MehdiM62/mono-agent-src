package n8nimport

import (
	"fmt"
	"strings"
)

// The Gemini, Ollama, MiniMax, Moonshot and Alibaba Cloud vendor nodes:
// text messages run on the vendor's local agent runtime (agent.ask, routed
// like the vendor's chat model sub-node); every other operation imports as
// the vendor's API package (app.gemini_api, …) with the user's own key or
// server. The Databricks AI Agent is n8n's tools agent and imports like the
// AI Agent node.

// vendorText describes a vendor node's text message operation.
type vendorText struct {
	route        string // the chat model sub-node type whose modelRoute runs it
	defaultModel string // n8n's default model ("" when the node requires one)
	messages     string // the messages list parameter
	system       string // the system message option
	// unsupported options/parameters agent.ask cannot honour.
	unsupported []string
	// shape is the JavaScript object built from the answer a (simplified),
	// raw the one for Simplify off.
	shape, raw string
}

var vendorTexts = map[string]vendorText{
	lc + "googleGemini": {route: lc + "lmChatGoogleGemini", defaultModel: "models/gemini-3-flash-preview",
		messages: "messages.values", system: "options.systemMessage",
		unsupported: []string{"builtInTools.googleSearch", "builtInTools.googleMaps", "builtInTools.urlContext",
			"builtInTools.fileSearch", "builtInTools.codeExecution", "options.codeExecution"},
		shape: `{ content: { role: "model", parts: [{ text: a }] }, mergedResponse: merged ? a : undefined }`,
		raw:   `{ candidates: [{ content: { role: "model", parts: [{ text: a }] }, mergedResponse: merged ? a : undefined }] }`},
	lc + "ollama": {route: lc + "lmChatOllama", messages: "messages.values", system: "options.system",
		shape: `{ content: a }`, raw: `{ message: { role: "assistant", content: a }, done: true }`},
	lc + "moonshot": {route: lc + "lmChatMoonshot", messages: "messages.values", system: "options.system",
		unsupported: []string{"addAttachments", "options.webSearch"},
		shape:       `{ content: a, merged_response: merged ? a : undefined }`,
		raw:         `{ choices: [{ index: 0, finish_reason: "stop", message: { role: "assistant", content: a } }], merged_response: merged ? a : undefined }`},
	lc + "minimax": {route: lc + "lmChatMinimax", defaultModel: "MiniMax-M3", messages: "messages.values", system: "options.system",
		shape: `{ content: a }`, raw: `{ choices: [{ index: 0, finish_reason: "stop", message: { role: "assistant", content: a } }] }`},
	lc + "alibabaCloud": {route: lc + "lmChatAlibabaCloud", defaultModel: "qwen3.5-flash", messages: "messages.messageValues",
		system: "options.system", unsupported: []string{"options.enableSearch"},
		shape: `{ content: a }`, raw: `{ content: a, model: model }`},
}

func init() {
	for t := range vendorTexts {
		Register(t, mapVendorNode)
	}
	Register(lc+"databricksAiAgent", mapDatabricksAgent)
}

// mapVendorNode maps the text message onto agent.ask and every other
// operation through the vendor's API package.
func mapVendorNode(c *MapContext) Mapping {
	res, op := resOp(c, "text", "message")
	if res == "text" && op == "message" {
		return mapVendorText(c, vendorTexts[c.Type])
	}
	a, ok := lookupApp(c.Type)
	if !ok {
		return c.OperationUnsupported(res, op)
	}
	for _, s := range c.SubNodes {
		c.Warn(s.Kind, s.Name, "tool %q (%s) is not called: the %s API package answers without n8n tools", s.Name, shortType(s.Type), a.PackageID)
	}
	return mapApp(a, c)
}

// mapVendorText runs the messages on the vendor's local agent runtime,
// shaped like n8n's answer.
func mapVendorText(c *MapContext, v vendorText) Mapping {
	cfg := map[string]interface{}{"output_key": "__n8n_answer"}
	model := c.Raw("modelId")
	if model == nil && v.defaultModel != "" {
		model = v.defaultModel
		if c.Type == lc+"minimax" && !c.version(1.1) {
			model = "MiniMax-M2.7"
		}
	}
	useModel(c, cfg, SubNode{Kind: "ai_languageModel", Name: c.NodeName, Type: v.route,
		Parameters: map[string]interface{}{"modelId": model}})
	var prompt []string
	for i, m := range c.paramList(v.messages) {
		text := fmt.Sprint(c.Translate(fmt.Sprintf("messages[%d]", i), m["content"]))
		if strings.TrimSpace(text) == "" {
			continue
		}
		if r := fmt.Sprint(m["role"]); r == "assistant" || r == "model" {
			text = "Assistant: " + text
		}
		prompt = append(prompt, text)
	}
	cfg["prompt"] = strings.Join(prompt, "\n\n")
	if sys := c.Str(v.system, ""); sys != "" {
		cfg["system_prompt"] = sys
	}
	for _, p := range v.unsupported {
		if c.Has(p) && c.Literal(p, "") != "false" {
			c.Warn(p, compactValue(c.Raw(p)), "is not available through agent.ask")
		}
	}
	for _, s := range c.SubNodes {
		c.Warn("tools", s.Name, "tool %q (%s) is not available: agent.ask answers without tools", s.Name, shortType(s.Type))
	}
	c.Ignore("simplify", "jsonOutput", "options", "binaryPropertyName", "legacyTextModel")
	if c.Has("options") {
		c.Note("sampling and token options are left to the agent runtime")
	}
	shape := v.shape
	if !c.Bool("simplify", true) {
		shape = v.raw
	}
	merged := c.Bool("options.includeMergedResponse", false)
	body := fmt.Sprintf("var merged = %t, model = %s;\n", merged, jsQuote(fmt.Sprint(cfg["model"]))) +
		"return items.map(function (it) { var a = it.json.__n8n_answer; return " + shape + "; });\n"
	return Mapping{Type: "agent.ask", Config: cfg, EntrySuffix: " (ask)",
		Chain: []ChainStep{{Type: "core.code", Config: codeConfig(wrapCode(body))}}}
}

// mapDatabricksAgent maps the Databricks AI Agent like the AI Agent; its
// MLflow trace export to the Databricks workspace is not done.
func mapDatabricksAgent(c *MapContext) Mapping {
	if c.Bool("exportTraces", false) {
		c.Warn("exportTraces", true, "runs are not exported to MLflow Tracing in Databricks")
	}
	c.Ignore("exportTraces", "experiment")
	return mapLLMNode(c, "output")
}
