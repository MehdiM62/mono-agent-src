package n8nimport

import (
	"fmt"
	"regexp"
	"strings"

	vectornodes "github.com/monoes/mono-agent/internal/nodes/vector"
)

// AI tools. agent.ask cannot call tools yet, so a tool attached to an agent
// (ai_tool) is not dropped: it is mapped like a node of its own and kept,
// disabled and unconnected, beside the agent. The values the agent would
// fill in ($fromAI(...), and the "input" of n8n's one-string tools) read the
// item, so the node runs as the tool would once something passes it the
// tool call's arguments as an item.
//
// n8n also generates a "<type>Tool" variant of every node usable as a
// tool (slackTool, httpRequestTool, …); it is mapped exactly like its base
// type, attached or not.

func init() {
	Register(lc+"agentTool", func(c *MapContext) Mapping {
		c.Ignore("toolDescription")
		return mapLLMNode(c, "output")
	})
	Register(lc+"toolCode", mapToolCode)
	Register(lc+"toolHttpRequest", mapToolHTTPRequest)
	Register(lc+"toolWorkflow", func(c *MapContext) Mapping {
		c.Ignore("name", "description", "noticeTemplateExample", "executeNotice")
		return mapExecuteWorkflow(c)
	})
	Register(lc+"toolCalculator", mapToolCalculator)
	Register(lc+"toolThink", mapToolThink)
	Register(lc+"toolWikipedia", mapToolWikipedia)
	Register(lc+"toolSerpApi", mapToolSerpAPI)
	Register(lc+"toolWolframAlpha", mapToolWolframAlpha)
	Register(lc+"toolSearXng", mapToolSearXng)
	Register(lc+"toolVectorStore", mapToolVectorStore)
	Register(lc+"mcpClientTool", mapMCPClientTool)
	Register(lc+"mcpRegistryClientTool", mapMCPClientTool)
}

// toolVariant points ctx at the base type of an n8n "<type>Tool" variant
// that has no mapping of its own, and reports whether it did.
func toolVariant(ctx *MapContext) bool {
	base := strings.TrimSuffix(ctx.Type, "Tool")
	if base == ctx.Type {
		return false
	}
	if _, ok := lookup(base); !ok {
		if _, ok := lookupApp(base); !ok {
			return false
		}
	}
	ctx.Type = base
	ignoreToolParams(ctx)
	ctx.Note("n8n's tool variant of %s: mapped like %s", shortType(base), shortType(base))
	return true
}

// ignoreToolParams marks the parameters n8n adds to a tool variant.
func ignoreToolParams(ctx *MapContext) {
	ctx.Ignore("toolDescription", "descriptionType", "toolAttribution")
}

// toolNodes names the nodes attached to another one as a tool. A Tool
// Executor's tool is left out: the executor runs it in its own chain.
func (b *builder) toolNodes(subs map[string][]SubNode) map[string]bool {
	out := map[string]bool{}
	for parent, list := range subs {
		if p := b.byName[parent]; p != nil && p.Type == lc+"toolExecutor" {
			continue
		}
		for _, s := range list {
			if s.Kind == "ai_tool" {
				out[s.Name] = true
			}
		}
	}
	return out
}

// keepTool maps a tool sub-node and places it disabled and unconnected.
func (b *builder) keepTool(n *n8nNode, parent string, subs map[string][]SubNode, nr *NodeReport) {
	ctx := newMapContext(n, b.rep.Workflow)
	ctx.SubNodes = subs[n.Name]
	m := b.runMapper(ctx, nr)
	first := len(b.out.Nodes)
	b.place(n, ctx, m, nr)
	for i := first; i < len(b.out.Nodes); i++ {
		b.out.Nodes[i].Disabled = true
	}
	nr.Issues = append(nr.Issues, Issue{Severity: SevInfo, Message: fmt.Sprintf(
		"tool of %q, kept as a disabled node: agent.ask cannot call tools yet. The tool call's arguments are read from the item", parent)})
}

// codeTool wraps a per-item JavaScript body that computes response from
// the tool's input, as n8n's one-string tools return a text answer.
func codeTool(inputExpr, body string) map[string]interface{} {
	return codeConfig(wrapCode("return items.map(function (it) {\n  var input = " + inputExpr + ";\n" + indentJS(body) +
		"  return { response: typeof response === 'string' ? response : JSON.stringify(response) };\n});\n"))
}

const toolInput = "it.json.input !== undefined ? it.json.input : it.json.query"

func mapToolCode(c *MapContext) Mapping {
	c.Ignore("name", "description", "noticeTemplateExample", "schemaType", "jsonSchemaExample", "inputSchema")
	if lang := c.Literal("language", "javaScript"); lang != "javaScript" {
		c.Ignore("pythonCode")
		c.Warn("language", lang, "Python code cannot run in mono-agent (core.code runs JavaScript)")
		return Mapping{Unmapped: true}
	}
	in := "(" + toolInput + ")"
	if c.Bool("specifyInputSchema", false) {
		in = "it.json" // a structured tool receives the arguments object
	}
	code := importCode(c, "jsCode", fmt.Sprint(c.Raw("jsCode")))
	body := "var query = input;\nvar response = (function (query) {\n" + indentJS(code) + "})(query);\n"
	c.Note("the code's answer is returned under \"response\"")
	return Mapping{Type: "core.code", Config: codeTool(in, body)}
}

func mapToolThink(c *MapContext) Mapping {
	c.Ignore("description")
	return Mapping{Type: "core.code", Config: codeTool(toolInput, "var response = input;\n")}
}

// mapToolCalculator evaluates the arithmetic expression the agent sends.
// Only numbers, operators, parentheses and Math functions are accepted;
// "^" is a power, as in n8n's calculator.
func mapToolCalculator(c *MapContext) Mapping {
	body := `var expr = String(input);
var own = function (n) { return Object.prototype.hasOwnProperty.call(Math, n); };
var names = /[A-Za-z_]+/g, m;
while ((m = names.exec(expr)) !== null) {
  if (m[0] !== 'e' && m[0] !== 'E' && !own(m[0]) && !own(m[0].toUpperCase())) {
    throw new Error('calculator: unknown name ' + m[0]);
  }
}
if (!/^[0-9A-Za-z_+\-*\/%^().,\s]*$/.test(expr)) { throw new Error('calculator: unsupported characters in ' + expr); }
var js = expr.replace(/\^/g, '**').replace(/[A-Za-z_]+/g, function (n) {
  if (n === 'e' || n === 'E') { return n; }
  return own(n) ? 'Math.' + n : 'Math.' + n.toUpperCase();
});
var response = String(new Function('return (' + js + ');')());
`
	return Mapping{Type: "core.code", Config: codeTool(toolInput, body)}
}

func mapToolWikipedia(c *MapContext) Mapping {
	c.Note("searches English Wikipedia and returns the top 3 pages' introductions (the API's JSON)")
	return Mapping{Type: "http.request", Config: map[string]interface{}{
		"method": "GET", "url": "https://en.wikipedia.org/w/api.php", "response_format": "json",
		"query_params": map[string]interface{}{"action": "query", "format": "json", "generator": "search",
			"gsrsearch": "{{ " + toolArg + " }}", "gsrlimit": "3", "prop": "extracts", "exintro": "1", "explaintext": "1"}}}
}

// toolArg is the template of a one-string tool's input.
const toolArg = "default $json.query $json.input"

func mapToolSerpAPI(c *MapContext) Mapping {
	q := map[string]interface{}{"engine": "google", "q": "{{ " + toolArg + " }}", "api_key": c.Secret("api_key", "serpApi")}
	for k, v := range c.paramMap("options") {
		q[k] = c.Translate("options."+k, v)
	}
	c.Ignore("options", "oldVersionNotice")
	return Mapping{Type: "http.request", Config: map[string]interface{}{
		"method": "GET", "url": "https://serpapi.com/search.json", "response_format": "json", "query_params": q}}
}

func mapToolWolframAlpha(c *MapContext) Mapping {
	return Mapping{Type: "http.request", Config: map[string]interface{}{
		"method": "GET", "url": "https://www.wolframalpha.com/api/v1/llm-api", "response_format": "text",
		"query_params": map[string]interface{}{"input": "{{ " + toolArg + " }}", "appid": c.Secret("app_id", "wolframAlphaApi")}}}
}

func mapToolSearXng(c *MapContext) Mapping {
	q := map[string]interface{}{"q": "{{ " + toolArg + " }}", "format": "json"}
	for n8n, ours := range map[string]string{"pageNumber": "pageno", "language": "language", "safesearch": "safesearch"} {
		if c.Has("options." + n8n) {
			q[ours] = c.Value("options." + n8n)
		}
	}
	c.Ignore("options.pageNumber", "options.language", "options.safesearch", "options.numResults")
	if c.Has("options.numResults") {
		c.Note("SearXNG returns its own page of results; numResults is not applied")
	}
	c.Warn("url", "searXngApi", "set url to your SearXNG instance's /search (the n8n credential's API URL)")
	return Mapping{Type: "http.request", Config: map[string]interface{}{
		"method": "GET", "url": "http://localhost:8080/search", "response_format": "json", "query_params": q}}
}

// mapToolVectorStore searches the attached vector store with the agent's
// question. n8n then had a model write an answer from the documents; the
// documents themselves are returned here.
func mapToolVectorStore(c *MapContext) Mapping {
	c.Ignore("name", "description")
	for _, s := range c.SubNodes {
		if s.Kind != "ai_vectorStore" {
			continue
		}
		params := withoutEmpty(s.Parameters)
		params["prompt"] = "={{ $json.input }}"
		known := vectornodes.N8nNodeMappings[s.Type].Params
		if _, ok := known["topK"]; ok && c.Has("topK") {
			params["topK"] = c.Raw("topK")
		}
		if _, ok := known["mode"]; ok {
			params["mode"] = "load"
		}
		res, err := vectornodes.CollapseN8nSubNodes(s.Type, params, c.vectorSubs(s.Children))
		if err != nil {
			c.issues = append(c.issues, Issue{Severity: SevError, Message: err.Error()})
			return Mapping{Unmapped: true}
		}
		for _, w := range res.Warnings {
			c.Note("%s", w)
		}
		cfg := c.translateConfig(res.Node.Config)
		if platform, credType := vectorCredential(s.Credentials); platform != "" {
			cfg["credential_id"] = c.Credential(platform, credType)
		}
		c.Note("returns the documents %s finds; n8n had a model answer from them", shortType(s.Type))
		return Mapping{Type: res.Node.Type, Config: cfg}
	}
	c.issues = append(c.issues, Issue{Severity: SevError, Message: "no vector store is attached to the tool"})
	return Mapping{Unmapped: true}
}

// mapMCPClientTool calls the tool the agent picks on an MCP server.
func mapMCPClientTool(c *MapContext) Mapping {
	endpoint := c.Str("endpointUrl", "")
	if endpoint == "" {
		endpoint = c.Str("sseEndpoint", "")
	}
	cfg := map[string]interface{}{"transport": "http", "url": endpoint, "operation": "call_tool",
		"tool": "{{ $json.tool }}", "arguments": "{{ json $json.arguments }}"}
	if t := c.Literal("serverTransport", "httpStreamable"); t != "httpStreamable" || c.Has("sseEndpoint") {
		c.Warn("serverTransport", t, "mcp.call speaks Streamable HTTP; a server that only offers SSE will not answer")
	}
	if a := c.Literal("authentication", "none"); a != "none" {
		c.Warn("authentication", a, "the n8n credential is not carried over: save the server with its headers in a vault secret and set server")
	}
	switch c.Literal("include", "all") {
	case "selected":
		c.Note("the agent may call only: %s", compactValue(c.Raw("includeTools")))
	case "except":
		c.Note("the agent may not call: %s", compactValue(c.Raw("excludeTools")))
	}
	c.Ignore("include", "includeTools", "excludeTools")
	if c.Has("options.timeout") {
		cfg["timeout"] = c.Float("options.timeout", 60000) / 1000
	}
	c.Note("calls the tool named in the item's \"tool\" with its \"arguments\"")
	return Mapping{Type: "mcp.call", Config: cfg}
}

var toolPlaceholder = regexp.MustCompile(`(^|[^{])\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// placeholders turns the {name} placeholders of n8n's HTTP Request tool
// into n8n expressions reading the tool call's argument.
func placeholders(v interface{}) interface{} {
	s, ok := v.(string)
	if !ok || !toolPlaceholder.MatchString(s) {
		return v
	}
	s = toolPlaceholder.ReplaceAllString(s, "$1{{ $$json.$2 }}")
	if !strings.HasPrefix(s, "=") {
		s = "=" + s
	}
	return s
}

// mapToolHTTPRequest rewrites the HTTP Request tool's parameters into the
// HTTP Request node's and maps that.
func mapToolHTTPRequest(c *MapContext) Mapping {
	p := map[string]interface{}{}
	for _, k := range []string{"method", "authentication", "nodeCredentialType", "genericAuthType"} {
		if v, ok := c.Params[k]; ok {
			p[k] = v
		}
	}
	p["url"] = placeholders(c.Params["url"])
	for _, part := range []struct{ send, specify, list, jsonKey, ours string }{
		{"sendQuery", "specifyQuery", "parametersQuery", "jsonQuery", "queryParameters"},
		{"sendHeaders", "specifyHeaders", "parametersHeaders", "jsonHeaders", "headerParameters"},
		{"sendBody", "specifyBody", "parametersBody", "jsonBody", "bodyParameters"},
	} {
		if c.Params[part.send] != true {
			continue
		}
		p[part.send] = true
		switch spec := c.Literal(part.specify, "keypair"); spec {
		case "json":
			p[part.specify] = "json"
			p[part.jsonKey] = placeholders(c.Params[part.jsonKey])
		case "model":
			c.Warn(part.specify, spec, "the agent wrote this part of the request in n8n; it is read from the item's %q", part.ours)
			p[part.specify] = "json"
			p[part.jsonKey] = "={{ JSON.stringify($json." + part.ours + ") }}"
		default:
			var rows []interface{}
			for _, r := range c.paramList(part.list + ".values") {
				name := fmt.Sprint(r["name"])
				val := placeholders(r["value"])
				if prov, _ := r["valueProvider"].(string); prov == "modelRequired" || prov == "modelOptional" {
					val = "={{ $json." + name + " }}"
					if !goIdent.MatchString(name) {
						val = "={{ $json['" + name + "'] }}"
					}
				}
				rows = append(rows, map[string]interface{}{"name": name, "value": val})
			}
			p[part.ours] = map[string]interface{}{"parameters": rows}
		}
	}
	if c.Literal("optimizeResponse", "") == "true" || c.Params["optimizeResponse"] == true {
		c.Note("response optimisation (fields, CSS selector, truncation) is not applied: the whole response is returned")
	}
	c.Ignore("toolDescription", "placeholderDefinitions", "placeholderNotice", "optimizeResponse", "responseType", "dataField",
		"fieldsToInclude", "fields", "cssSelector", "onlyContent", "elementsToOmit", "truncateResponse", "maxLength",
		"sendQuery", "specifyQuery", "parametersQuery", "jsonQuery", "sendHeaders", "specifyHeaders", "parametersHeaders",
		"jsonHeaders", "sendBody", "specifyBody", "parametersBody", "jsonBody", "googleApiWarning")
	c.Params, c.TypeVersion = p, 4.2
	return MapHTTPRequest(c)
}
