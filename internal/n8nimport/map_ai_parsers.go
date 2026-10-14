package n8nimport

// n8n's Structured Output Parser (and an Auto-fixing Output Parser around
// one) becomes an ai.output_parser sub-node: the JSON shape the agent
// answers with, from an example or a JSON Schema.

// n8n's defaults for the parser's example and schema, which exports leave
// out when they were not changed.
const (
	parserDefaultExample = "{\n\t\"state\": \"California\",\n\t\"cities\": [\"Los Angeles\", \"San Francisco\", \"San Diego\"]\n}"
	parserDefaultSchema  = "{\n\t\"type\": \"object\",\n\t\"properties\": {\n\t\t\"state\": {\"type\": \"string\"},\n\t\t\"cities\": {\"type\": \"array\", \"items\": {\"type\": \"string\"}}\n\t}\n}"
)

// attachOutputParser keeps output parser s as an ai.output_parser sub-node
// of cfg's node, or reports a parser it cannot stand in for.
func attachOutputParser(c *MapContext, cfg map[string]interface{}, s SubNode) {
	parser := s
	switch shortType(s.Type) {
	case "outputParserStructured":
	case "outputParserAutofixing":
		inner := firstKind(s.Children, "ai_outputParser")
		if inner == nil || shortType(inner.Type) != "outputParserStructured" {
			c.Warn("outputParser", s.Name, "structured output parsing (%s) is not applied: it wraps no Structured Output Parser", shortType(s.Type))
			return
		}
		c.Warn("outputParser", s.Name, "%s: an answer that does not fit the schema fails the item; it is not sent back to a model to fix",
			shortType(s.Type))
		parser = *inner
	default:
		c.Warn("outputParser", s.Name, "structured output parsing (%s) is not applied; ask for JSON in the prompt", shortType(s.Type))
		return
	}
	c.attach(s.Name, s.Kind, "ai.output_parser", structuredParserConfig(c, parser), cfg)
}

// structuredParserConfig is ai.output_parser's config for a Structured
// Output Parser: versions up to 1.1 take a JSON Schema (jsonSchema), later
// ones an example (fromJson, the default) or a schema (manual, inputSchema).
func structuredParserConfig(c *MapContext, s SubNode) map[string]interface{} {
	p := s.Parameters
	text := func(key, def string) interface{} {
		v := p[key]
		if v == nil || v == "" {
			v = def
		}
		return c.Translate(s.Name+"."+key, v)
	}
	if p["autoFix"] == true {
		c.Warn("outputParser", s.Name, "auto-fix is not applied: an answer that does not fit the schema fails the item")
	}
	if s.TypeVersion <= 1.1 {
		return map[string]interface{}{"schema_type": "schema", "json_schema": text("jsonSchema", parserDefaultSchema)}
	}
	if t, _ := p["schemaType"].(string); t == "manual" {
		return map[string]interface{}{"schema_type": "schema", "json_schema": text("inputSchema", parserDefaultSchema)}
	}
	if s.TypeVersion < 1.3 {
		c.Note("output parser %q: every key of the JSON example is required (n8n %v left them optional)", s.Name, s.TypeVersion)
	}
	return map[string]interface{}{"schema_type": "example", "json_example": text("jsonSchemaExample", parserDefaultExample)}
}
