package n8nimport

import "fmt"

// n8n's Form node becomes core.form: the next page, or the ending, of the
// browser session the workflow's Form Trigger started. Fields keep n8n's
// keys (core.form reads them as the Wait node's form does), so answers are
// keyed as n8n keyed them.

func init() {
	Register("n8n-nodes-base.form", mapFormPage)
}

// formPageFieldKeys are the per-field keys core.form reads.
var formPageFieldKeys = []string{"fieldLabel", "fieldType", "requiredField", "placeholder", "fieldOptions", "multiselect"}

func mapFormPage(c *MapContext) Mapping {
	cfg := map[string]interface{}{}
	switch op := c.Literal("operation", "page"); op {
	case "completion":
		cfg["operation"] = "completion"
		with := c.Literal("respondWith", "text")
		switch with {
		case "text", "showText", "redirect":
		case "returnBinary":
			c.Warn("respondWith", with, "core.form cannot send a file as the form's ending; it shows the completion title and message instead")
			c.Ignore("inputDataFieldName")
			with = "text"
		default:
			c.Warn("respondWith", with, "is not known; the form shows the completion title and message")
			with = "text"
		}
		cfg["respondWith"] = with
		switch with {
		case "redirect":
			cfg["redirectUrl"] = c.Str("redirectUrl", "")
		case "showText":
			cfg["responseText"] = c.Str("responseText", "")
		default:
			cfg["completionTitle"] = c.Str("completionTitle", "")
			if c.Has("completionMessage") {
				cfg["completionMessage"] = c.Str("completionMessage", "")
			}
		}
		if v := c.Str("options.formTitle", ""); v != "" {
			cfg["formTitle"] = v
		}
		c.Ignore("options.appendAttribution")
	case "page":
		cfg["operation"] = "page"
		var fields []interface{}
		for i, f := range c.n8nFormFields() {
			if ff := c.formPageField(i, f); ff != nil {
				fields = append(fields, ff)
			}
		}
		cfg["formFields"] = fields
		if len(fields) == 0 {
			c.issues = append(c.issues, Issue{Severity: SevError, Param: "formFields",
				Message: "the page has no field core.form can show; add at least one before running"})
		}
		for _, k := range []string{"formTitle", "formDescription", "buttonLabel"} {
			if v := c.Str("options."+k, ""); v != "" {
				cfg[k] = v
			}
		}
	default:
		c.Warn("operation", op, "is not page or completion; the node shows a page")
		cfg["operation"] = "page"
	}
	if c.Bool("limitWaitTime", false) {
		cfg["limitWaitTime"] = true
		cfg["limitType"] = c.Literal("limitType", "afterTimeInterval")
		cfg["resumeAmount"] = c.Float("resumeAmount", 1)
		cfg["resumeUnit"] = c.Literal("resumeUnit", "hours")
		if c.Has("maxDateAndTime") {
			cfg["maxDateAndTime"] = c.Str("maxDateAndTime", "")
		}
	} else {
		c.Ignore("limitType", "resumeAmount", "resumeUnit", "maxDateAndTime")
	}
	if c.Has("options.customCss") {
		c.Warn("options.customCss", "(custom CSS)", "form pages use mono-agent's own style; custom CSS is not applied")
	}
	c.Ignore("triggerNotice")
	return Mapping{Type: "core.form", Config: cfg}
}

// formPageField keeps one n8n field for core.form, or reports why it was
// left out and returns nil. From version 2.4 a field's fieldName keys its
// answer, as in n8n.
func (c *MapContext) formPageField(i int, f map[string]interface{}) map[string]interface{} {
	param := fmt.Sprintf("formFields[%d]", i)
	switch typ, _ := f["fieldType"].(string); typ {
	case "file":
		c.Warn(param, f["fieldLabel"], "file upload fields are not supported by core.form; the field was left out of the page")
		return nil
	case "html":
		c.Warn(param, f["elementName"], "custom HTML is not supported by core.form; the block was left out of the page")
		return nil
	case "hiddenField":
		c.Warn(param, f["fieldName"], "hidden fields are not supported by core.form; the field was left out (add its value %v after the page with a Set step if the workflow reads it)", f["fieldValue"])
		return nil
	}
	out := map[string]interface{}{}
	for _, k := range formPageFieldKeys {
		if v, ok := f[k]; ok {
			out[k] = c.Translate(param+"."+k, v)
		}
	}
	if name, _ := f["fieldName"].(string); name != "" && c.version(2.4) {
		out["fieldName"] = name
	}
	if out["fieldLabel"] == nil && out["fieldName"] == nil {
		c.Warn(param, nil, "the field has no label; it was left out of the page")
		return nil
	}
	if v, ok := f["defaultValue"]; ok && !isEmptyValue(v) {
		c.Warn(param+".defaultValue", v, "core.form fields start empty; the default value is not filled in")
	}
	return out
}
