package n8nimport

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The n8n Form Trigger becomes trigger.form: the webhook server serves the
// form page at /form/<path> and each valid submission starts a run with
// the answers keyed by field label, plus submittedAt and formMode, as n8n
// emitted them. Field types trigger.form has no counterpart for are left
// out of the form and reported.

// formFieldTypeMap maps n8n field types to trigger.form's one for one.
var formFieldTypeMap = map[string]string{
	"text": "text", "textarea": "textarea", "email": "email", "number": "number",
	"date": "date", "dropdown": "dropdown", "checkbox": "checkbox",
}

// n8nFormFields reads the form's field definitions: the field list, or the
// JSON definition of defineForm "json".
func (c *MapContext) n8nFormFields() []map[string]interface{} {
	if c.Literal("defineForm", "fields") != "json" {
		c.Ignore("jsonOutput")
		return c.paramList("formFields.values")
	}
	c.Ignore("formFields")
	text, _ := c.Raw("jsonOutput").(string)
	if strings.HasPrefix(text, "=") {
		c.Warn("jsonOutput", text, "a form defined by an expression cannot be read when importing; write the fields into the trigger's formFields")
		return nil
	}
	var raw []map[string]interface{}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		c.Warn("jsonOutput", text, "is not a JSON list of fields; write the fields into the trigger's formFields")
		return nil
	}
	return raw
}

// formFieldOptions reads a field's choices: n8n's {values: [{option}]}.
func formFieldOptions(f map[string]interface{}) []interface{} {
	var out []interface{}
	opts, _ := f["fieldOptions"].(map[string]interface{})
	list, _ := opts["values"].([]interface{})
	for _, o := range list {
		if m, ok := o.(map[string]interface{}); ok {
			if s, ok := m["option"].(string); ok && s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// formField converts one n8n field to a trigger.form field, or reports why
// it was left out and returns nil. The field is keyed by what n8n keyed its
// answer by (the label, or from version 2.4 the field name when set), so
// expressions downstream keep working.
func (c *MapContext) formField(i int, f map[string]interface{}) map[string]interface{} {
	label, _ := f["fieldLabel"].(string)
	name, _ := f["fieldName"].(string)
	id := label
	if (c.version(2.4) && name != "") || id == "" {
		id = name
	}
	typ, _ := f["fieldType"].(string)
	if typ == "" {
		typ = "text"
	}
	param := fmt.Sprintf("formFields[%d]", i)
	switch typ {
	case "file":
		c.Warn(param, id, "file upload fields are not supported by trigger.form; the field was left out of the form")
		return nil
	case "html":
		c.Warn(param, f["elementName"], "custom HTML is not supported by trigger.form; the block was left out of the form")
		return nil
	case "hiddenField":
		c.Warn(param, id, "hidden fields are not supported by trigger.form; the field was left out (add its value %v after the trigger with a Set step if the workflow reads it)", f["fieldValue"])
		return nil
	}
	if id == "" {
		c.Warn(param, nil, "the field has no label; it was left out of the form")
		return nil
	}
	out := map[string]interface{}{"fieldLabel": id}
	options := formFieldOptions(f)
	multi, _ := f["multiselect"].(bool)
	switch mono, ok := formFieldTypeMap[typ]; {
	case typ == "dropdown" && multi:
		out["fieldType"] = "checkbox"
		c.Note("%s: the multi-select dropdown %q became a list of checkboxes (its answer is still a list)", param, id)
	case typ == "radio":
		out["fieldType"] = "dropdown"
		c.Note("%s: the radio buttons %q became a dropdown (its answer is still the chosen option)", param, id)
	case typ == "password":
		out["fieldType"] = "text"
		c.Warn(param, id, "password fields are shown as plain text inputs by trigger.form")
	case ok:
		out["fieldType"] = mono
	default:
		out["fieldType"] = "text"
		c.Warn(param, typ, "is not a field type trigger.form knows; %q became a text input", id)
	}
	if len(options) > 0 && (out["fieldType"] == "dropdown" || out["fieldType"] == "checkbox") {
		out["fieldOptions"] = options
	}
	if req, _ := f["requiredField"].(bool); req {
		out["requiredField"] = true
	}
	if ph, _ := f["placeholder"].(string); ph != "" {
		out["placeholder"] = ph
	}
	if id != label && label != "" {
		c.Note("%s: the form shows the field name %q, the key its answer is stored under, instead of the label %q", param, id, label)
	}
	if v, ok := f["defaultValue"]; ok && !isEmptyValue(v) {
		c.Warn(param+".defaultValue", v, "trigger.form fields start empty; the default value of %q is not filled in", id)
	}
	if v, _ := f["formatDate"].(string); v != "" {
		c.Warn(param+".formatDate", v, "dates arrive as YYYY-MM-DD; reformat %q downstream", id)
	}
	if v, _ := f["limitSelection"].(string); v != "" && v != "unlimited" {
		c.Warn(param+".limitSelection", v, "the number of options picked for %q is not checked", id)
	}
	return out
}

// mapFormTrigger maps n8n's Form Trigger to trigger.form.
func mapFormTrigger(c *MapContext) Mapping {
	path := strings.Trim(c.Literal("path", ""), "/")
	if path == "" {
		path = strings.Trim(c.Literal("options.path", ""), "/")
	}
	if path == "" {
		path = webhookPath(c)
	}
	cfg := map[string]interface{}{"path": path}
	if v := c.Literal("formTitle", ""); v != "" {
		cfg["formTitle"] = v
	}
	if v := c.Literal("formDescription", ""); v != "" {
		cfg["formDescription"] = v
	}
	if v := c.Literal("options.buttonLabel", ""); v != "" {
		cfg["buttonLabel"] = v
	}

	var fields []interface{}
	seen := map[string]bool{}
	for i, f := range c.n8nFormFields() {
		ff := c.formField(i, f)
		if ff == nil {
			continue
		}
		if label := ff["fieldLabel"].(string); seen[label] {
			c.Warn(fmt.Sprintf("formFields[%d]", i), label, "two fields share this label; the second was left out (trigger.form keys answers by label)")
			continue
		}
		seen[ff["fieldLabel"].(string)] = true
		fields = append(fields, ff)
	}
	cfg["formFields"] = fields
	if len(fields) == 0 {
		c.issues = append(c.issues, Issue{Severity: SevError, Param: "formFields",
			Message: "the form has no field trigger.form can show; add at least one before activating"})
	}

	c.formResponse(cfg)
	switch auth := c.Literal("authentication", "none"); auth {
	case "none":
	default:
		c.Warn("authentication", auth, "trigger.form has no sign-in; the form at /form/%s is open to anyone who can reach the webhook server", path)
	}
	if v := c.Literal("options.ipWhitelist", ""); v != "" {
		c.Warn("options.ipWhitelist", v, "the form does not restrict callers by IP")
	}
	if c.Bool("options.ignoreBots", false) {
		c.Warn("options.ignoreBots", true, "the form does not filter out bots")
	}
	if v := c.Literal("options.customCss", ""); v != "" {
		c.Warn("options.customCss", "(custom CSS)", "the form page uses mono-agent's own style; custom CSS is not applied")
	}
	if c.Bool("options.useWorkflowTimezone", false) {
		c.Note("submittedAt is in UTC (n8n used the workflow's time zone)")
	}
	c.Ignore("formNotice", "requireExecuteAccess", "options.appendAttribution", "options.includeUserInOutput")
	c.Note("the form is served at /form/%s on the webhook server while the workflow is active", path)
	return Mapping{Type: "trigger.form", Config: cfg}
}

// formResponse maps what the page shows after a submission. trigger.form
// answers as soon as the run has started, so n8n's "when the workflow
// finishes" and Respond to Webhook modes show the completion text.
func (c *MapContext) formResponse(cfg map[string]interface{}) {
	switch mode := c.Literal("responseMode", "onReceived"); mode {
	case "onReceived":
	case "lastNode", "responseNode":
		c.Warn("responseMode", mode, "the form page shows its completion text (or redirect) as soon as the run starts; it does not wait for the workflow's result")
	default:
		c.Warn("responseMode", mode, "is not known; the form page shows its completion text as soon as the run starts")
	}
	respondWith := c.Literal("options.respondWithOptions.values.respondWith", "text")
	text := c.Literal("options.respondWithOptions.values.formSubmittedText", "")
	if text == "" {
		text = c.Literal("options.formSubmittedText", "")
	}
	switch respondWith {
	case "redirect":
		cfg["respondWith"] = "redirect"
		cfg["redirectUrl"] = c.Literal("options.respondWithOptions.values.redirectUrl", "")
	case "text":
		if text != "" {
			cfg["responseText"] = text
		}
	default:
		c.Warn("options.respondWithOptions", respondWith, "is not text or redirect; the form shows its default completion text")
	}
}
