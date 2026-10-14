package n8nimport

import "strings"

// mapCodePython maps the Code node's Python (language "python", n8n's
// Pyodide mode, or "pythonNative", its task runner) to core.code with
// language python, which runs the code unchanged with the system python3
// and offers both modes' names (_items/_item, _input, _json, _('Node')).
func mapCodePython(c *MapContext) Mapping {
	code, _ := c.Raw("pythonCode").(string)
	c.Ignore("notice")
	if strings.HasPrefix(code, "=") {
		c.Warn("pythonCode", code, "code set by an expression cannot be imported")
		code = "raise Exception(\"the n8n code was set by an expression\")"
	}
	cfg := codeConfig(code)
	cfg["language"] = "python"
	if c.Literal("mode", "runOnceForAllItems") == "runOnceForEachItem" {
		cfg["mode"] = "runOnceForEachItem"
	}
	return Mapping{Type: "core.code", Config: cfg}
}
