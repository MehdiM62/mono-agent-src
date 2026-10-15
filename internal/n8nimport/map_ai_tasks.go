package n8nimport

import (
	"encoding/json"
	"fmt"
	"strings"
)

// LangChain task chains (Information Extractor, Sentiment Analysis, Text
// Classifier) become agent.ask with an output schema; the routing ones
// add a core.switch on the answer, one output per category as in n8n.

func init() {
	Register(lc+"informationExtractor", mapInformationExtractor)
	Register(lc+"sentimentAnalysis", mapSentimentAnalysis)
	Register(lc+"textClassifier", mapTextClassifier)
	Register(lc+"mcpClient", mapMCPClient)
	Register(lc+"anthropic", mapAnthropic)
}

func mapInformationExtractor(c *MapContext) Mapping {
	c.Ignore("options.batching", "options.enableAutoFixing")
	var schema interface{}
	switch st := c.Literal("schemaType", "fromAttributes"); st {
	case "fromAttributes":
		props, required := map[string]interface{}{}, []interface{}{}
		for _, a := range c.paramList("attributes.attributes") {
			name := fmt.Sprint(a["name"])
			typ := map[string]string{"number": "number", "boolean": "boolean"}[fmt.Sprint(a["type"])]
			if typ == "" {
				typ = "string" // string and date (n8n asks for an ISO date text)
			}
			p := map[string]interface{}{"type": typ}
			if d, _ := a["description"].(string); d != "" {
				p["description"] = d
			}
			props[name] = p
			if a["required"] == true {
				required = append(required, name)
			}
		}
		schema = map[string]interface{}{"type": "object", "properties": props, "required": required}
	case "fromJson":
		if ex := c.jsonText("jsonSchemaExample"); ex != nil {
			schema = schemaFromExample(ex)
		}
	case "manual":
		schema = c.jsonText("inputSchema")
	default:
		return c.OperationUnsupported("", st)
	}
	if schema == nil {
		schema = map[string]interface{}{"type": "object"}
	}
	prompt := "Extract the requested information from the text below. Answer with JSON matching the schema; " +
		"leave out values the text does not give.\n\nText:\n" + c.Str("text", "")
	cfg := map[string]interface{}{"prompt": prompt, "output_key": "output", "output_schema": schema}
	if sys := c.Str("options.systemPromptTemplate", ""); sys != "" {
		cfg["system_prompt"] = sys
	}
	applyModel(c, cfg)
	c.Note("the extracted object is added under \"output\"; n8n replaced the item with {output}")
	return Mapping{Type: "agent.ask", Config: cfg}
}

func mapSentimentAnalysis(c *MapContext) Mapping {
	c.Ignore("detailedResultsNotice", "options.enableAutoFixing", "options.batching")
	cats := commaList(c.Literal("options.categories", "Positive, Neutral, Negative"))
	props := map[string]interface{}{"category": map[string]interface{}{"type": "string", "enum": toAny(cats)}}
	if c.Bool("options.includeDetailedResults", false) {
		props["strength"] = map[string]interface{}{"type": "number", "description": "how strong the sentiment is, 0 to 1"}
		props["confidence"] = map[string]interface{}{"type": "number", "description": "how sure you are, 0 to 1"}
	}
	prompt := "Decide the sentiment of the text below: one of " + strings.Join(cats, ", ") + ".\n\nText:\n" + c.Str("inputText", "")
	cfg := map[string]interface{}{"prompt": prompt, "output_key": "sentimentAnalysis",
		"output_schema": map[string]interface{}{"type": "object", "properties": props, "required": []interface{}{"category"}}}
	if sys := c.Str("options.systemPromptTemplate", ""); sys != "" {
		cfg["system_prompt"] = strings.ReplaceAll(sys, "{categories}", strings.Join(cats, ", "))
	}
	applyModel(c, cfg)
	return routeOnAnswer(cfg, "{{ $json.sentimentAnalysis.category }}", cats)
}

func mapTextClassifier(c *MapContext) Mapping {
	c.Ignore("options.enableAutoFixing", "options.batching")
	var cats []string
	var lines []string
	for _, cat := range c.paramList("categories.categories") {
		name := fmt.Sprint(cat["category"])
		cats = append(cats, name)
		line := "- " + name
		if d, _ := cat["description"].(string); d != "" {
			line += ": " + d
		}
		lines = append(lines, line)
	}
	other := c.Literal("options.fallback", "discard") == "other"
	choices := cats
	if other {
		choices = append(append([]string(nil), cats...), "Other")
		lines = append(lines, "- Other: none of the categories above fits")
	}
	if c.Bool("options.multiClass", false) {
		c.Warn("options.multiClass", true, "each item goes to one category only")
	}
	prompt := "Classify the text below into exactly one of these categories:\n" + strings.Join(lines, "\n") +
		"\n\nText:\n" + c.Str("inputText", "")
	cfg := map[string]interface{}{"prompt": prompt, "output_key": "__n8n_class",
		"output_schema": map[string]interface{}{"type": "object", "required": []interface{}{"category"},
			"properties": map[string]interface{}{"category": map[string]interface{}{"type": "string", "enum": toAny(choices)}}}}
	if sys := c.Str("options.systemPromptTemplate", ""); sys != "" {
		cfg["system_prompt"] = strings.ReplaceAll(sys, "{categories}", strings.Join(choices, ", "))
	}
	applyModel(c, cfg)
	c.Note("the chosen category stays on the item in __n8n_class")
	return routeOnAnswer(cfg, "{{ $json.__n8n_class.category }}", choices)
}

// routeOnAnswer chains a core.switch after agent.ask: one output handle
// per category, in n8n's output order; other answers are dropped.
func routeOnAnswer(cfg map[string]interface{}, field string, cats []string) Mapping {
	return Mapping{Type: "agent.ask", Config: cfg, EntrySuffix: " (ask)", Outputs: cats,
		Chain: []ChainStep{{Type: "core.switch", Config: map[string]interface{}{
			"field": field, "cases": toAny(cats), "default_handle": "none"}}}}
}

// jsonText parses a JSON-text parameter; text that is not JSON is
// reported and gives nil.
func (c *MapContext) jsonText(param string) interface{} {
	var v interface{}
	if err := json.Unmarshal([]byte(c.Literal(param, "{}")), &v); err != nil {
		c.Warn(param, c.Raw(param), "is not valid JSON (%v); the answer is not checked against a schema", err)
		return nil
	}
	return v
}

func toAny(list []string) []interface{} {
	out := make([]interface{}, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

// schemaFromExample builds a JSON schema from an example value, as n8n's
// "Generate From JSON Example" does: every key of the example is required.
func schemaFromExample(v interface{}) map[string]interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		props, required := map[string]interface{}{}, []interface{}{}
		for _, k := range sortedKeys(x) {
			props[k] = schemaFromExample(x[k])
			required = append(required, k)
		}
		return map[string]interface{}{"type": "object", "properties": props, "required": required}
	case []interface{}:
		s := map[string]interface{}{"type": "array"}
		if len(x) > 0 {
			s["items"] = schemaFromExample(x[0])
		}
		return s
	case float64:
		return map[string]interface{}{"type": "number"}
	case bool:
		return map[string]interface{}{"type": "boolean"}
	case nil:
		return map[string]interface{}{"type": "null"}
	}
	return map[string]interface{}{"type": "string"}
}

// mapMCPClient calls one tool of an MCP server over HTTP with mcp.call.
func mapMCPClient(c *MapContext) Mapping {
	cfg := map[string]interface{}{"transport": "http", "url": c.Str("endpointUrl", ""), "operation": "call_tool",
		"tool": fmt.Sprint(c.Value("tool"))}
	if t := c.Literal("serverTransport", "httpStreamable"); t != "httpStreamable" {
		c.Warn("serverTransport", t, "mcp.call speaks Streamable HTTP; a server that only offers SSE will not answer")
	}
	if a := c.Literal("authentication", "none"); a != "none" {
		c.Warn("authentication", a, "the n8n credential is not carried over: save the server with its headers in a vault secret and set server")
	}
	var args interface{}
	if c.Literal("inputMode", "manual") == "json" {
		args = c.Str("jsonInput", "{}")
	} else {
		v, _ := c.paramMap("parameters")["value"].(map[string]interface{})
		if v == nil {
			v = map[string]interface{}{}
		}
		raw, _ := json.Marshal(c.Translate("parameters", v))
		args = string(raw)
	}
	cfg["arguments"] = args
	if c.Has("options.timeout") {
		cfg["timeout"] = c.Float("options.timeout", 60000) / 1000
	}
	if c.Bool("options.convertToBinary", true) {
		c.Note("images and audio in the tool result stay base64 in the JSON (n8n turned them into files)")
	}
	return Mapping{Type: "mcp.call", Config: cfg}
}

// mapAnthropic maps the Anthropic node's text message onto agent.ask with
// the local claude runtime, shaped like n8n's simplified answer.
func mapAnthropic(c *MapContext) Mapping {
	res, op := resOp(c, "text", "message")
	if res != "text" || op != "message" {
		return c.OperationUnsupported(res, op)
	}
	cfg := map[string]interface{}{"runtime": "claude", "output_key": "__n8n_answer"}
	if m := fmt.Sprint(c.Value("modelId")); m != "" && m != "<nil>" {
		cfg["model"] = m
	}
	var prompt []string
	for i, m := range c.paramList("messages.values") {
		text := fmt.Sprint(c.Translate(fmt.Sprintf("messages[%d]", i), m["content"]))
		if m["role"] == "assistant" {
			text = "Assistant: " + text
		}
		prompt = append(prompt, text)
	}
	cfg["prompt"] = strings.Join(prompt, "\n\n")
	if sys := c.Str("options.system", ""); sys != "" {
		cfg["system_prompt"] = sys
	}
	for _, p := range []string{"addAttachments", "options.codeExecution", "options.webSearch"} {
		if c.Bool(p, false) {
			c.Warn(p, true, "is not available through agent.ask")
		}
	}
	c.Ignore("attachmentsInputType", "attachmentsUrls", "binaryPropertyName", "options.maxUses", "options.allowedDomains",
		"options.promptCaching", "options.maxTokens", "options.temperature", "options.topP", "options.topK", "options.maxToolsIterations")
	shape := "{ content: [{ type: \"text\", text: a }] }"
	switch {
	case !c.Bool("simplify", true):
		shape = "{ type: \"message\", role: \"assistant\", content: [{ type: \"text\", text: a }], merged_response: a }"
	case c.Bool("options.includeMergedResponse", false):
		shape = "{ content: [{ type: \"text\", text: a }], merged_response: a }"
	}
	body := "return items.map(function (it) { var a = it.json.__n8n_answer; return " + shape + "; });\n"
	return Mapping{Type: "agent.ask", Config: cfg, EntrySuffix: " (ask)",
		Chain: []ChainStep{{Type: "core.code", Config: codeConfig(wrapCode(body))}}}
}
