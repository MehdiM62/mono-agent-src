package n8nimport

// The Gemini, Ollama, MiniMax, Moonshot and Alibaba Cloud vendor nodes
// import as their API packages (app.gemini_api, …) with the user's own key
// or server; the Databricks AI Agent is n8n's tools agent and imports like
// the AI Agent node.

func init() {
	for _, t := range []string{"googleGemini", "ollama", "minimax", "moonshot", "alibabaCloud"} {
		Register(lc+t, mapVendorPackage)
	}
	Register(lc+"databricksAiAgent", mapDatabricksAgent)
}

// mapVendorPackage maps the node through its API package and reports the
// tools attached to it: the package answers without calling them.
func mapVendorPackage(c *MapContext) Mapping {
	a, ok := lookupApp(c.Type)
	if !ok {
		return c.OperationUnsupported(c.Literal("resource", ""), c.Literal("operation", ""))
	}
	for _, s := range c.SubNodes {
		c.Warn(s.Kind, s.Name, "tool %q (%s) is not called: the %s API package answers without n8n tools", s.Name, shortType(s.Type), a.PackageID)
	}
	return mapApp(a, c)
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
