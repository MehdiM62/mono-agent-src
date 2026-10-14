package workflow

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// TriggerNodeTypeForm is the form trigger: the webhook server serves an
// HTML form at /form/<path>, and each submission starts a run with the
// answers as the item.
const TriggerNodeTypeForm = "trigger.form"

// Form field types (config formFields[].fieldType).
var formFieldTypes = map[string]bool{
	"text": true, "email": true, "number": true, "textarea": true,
	"dropdown": true, "date": true, "checkbox": true,
}

// formField is one question of a form.
type formField struct {
	Label       string
	Type        string
	Placeholder string
	Options     []string
	Required    bool
}

// formSpec is a trigger.form node's config.
type formSpec struct {
	Title        string
	Description  string
	ButtonLabel  string
	Fields       []formField
	RespondWith  string // "text" or "redirect"
	ResponseText string
	RedirectURL  string
}

const defaultFormResponse = "Thanks, your response has been recorded."

// parseFormSpec reads a trigger.form config. Field keys follow n8n's Form
// Trigger (fieldLabel, fieldType, fieldOptions, requiredField); label and
// options are accepted too. formFields may be a list or its JSON text.
func parseFormSpec(cfg map[string]interface{}) (*formSpec, error) {
	str := func(k string) string { s, _ := cfg[k].(string); return strings.TrimSpace(s) }
	spec := &formSpec{
		Title: str("formTitle"), Description: str("formDescription"), ButtonLabel: str("buttonLabel"),
		RespondWith: str("respondWith"), ResponseText: str("responseText"), RedirectURL: str("redirectUrl"),
	}
	if spec.ButtonLabel == "" {
		spec.ButtonLabel = "Submit"
	}
	switch spec.RespondWith {
	case "", "text":
		spec.RespondWith = "text"
		if spec.ResponseText == "" {
			spec.ResponseText = defaultFormResponse
		}
	case "redirect":
		u, err := url.Parse(spec.RedirectURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("trigger.form: respondWith redirect needs an http(s) redirectUrl")
		}
	default:
		return nil, fmt.Errorf("trigger.form: respondWith %q is not one of text, redirect", spec.RespondWith)
	}

	raw := cfg["formFields"]
	if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
		if err := json.Unmarshal([]byte(s), &raw); err != nil {
			return nil, fmt.Errorf("trigger.form: formFields is not JSON: %w", err)
		}
	}
	if m, ok := raw.(map[string]interface{}); ok { // n8n: {values: [...]}
		raw = m["values"]
	}
	list, _ := raw.([]interface{})
	if len(list) == 0 {
		return nil, fmt.Errorf("trigger.form: formFields needs at least one field")
	}
	seen := map[string]bool{}
	for i, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("trigger.form: formFields[%d] is not an object", i)
		}
		f, err := parseFormField(m)
		if err != nil {
			return nil, fmt.Errorf("trigger.form: formFields[%d]: %w", i, err)
		}
		if seen[f.Label] {
			return nil, fmt.Errorf("trigger.form: two fields are labelled %q", f.Label)
		}
		seen[f.Label] = true
		spec.Fields = append(spec.Fields, f)
	}
	return spec, nil
}

func parseFormField(m map[string]interface{}) (formField, error) {
	str := func(keys ...string) string {
		for _, k := range keys {
			if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
		return ""
	}
	f := formField{Label: str("fieldLabel", "label"), Type: str("fieldType", "type"), Placeholder: str("placeholder")}
	if f.Label == "" {
		return f, fmt.Errorf("fieldLabel is required")
	}
	if f.Type == "" {
		f.Type = "text"
	}
	if !formFieldTypes[f.Type] {
		return f, fmt.Errorf("fieldType %q is not one of text, email, number, textarea, dropdown, date, checkbox", f.Type)
	}
	f.Required, _ = m["requiredField"].(bool)
	if b, ok := m["required"].(bool); ok {
		f.Required = f.Required || b
	}
	f.Options = formOptions(m["fieldOptions"])
	if len(f.Options) == 0 {
		f.Options = formOptions(m["options"])
	}
	if f.Type == "dropdown" && len(f.Options) == 0 {
		return f, fmt.Errorf("dropdown %q needs options", f.Label)
	}
	return f, nil
}

// formOptions reads options as a list of strings, a comma-separated string,
// or n8n's {values: [{option}]}.
func formOptions(v interface{}) []string {
	var out []string
	switch t := v.(type) {
	case string:
		for _, o := range strings.Split(t, ",") {
			if o = strings.TrimSpace(o); o != "" {
				out = append(out, o)
			}
		}
	case map[string]interface{}:
		return formOptions(t["values"])
	case []interface{}:
		for _, item := range t {
			switch o := item.(type) {
			case string:
				out = append(out, o)
			case map[string]interface{}:
				if s, ok := o["option"].(string); ok {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// formInputName is the HTML name of field i.
func formInputName(i int) string { return "field-" + strconv.Itoa(i) }

// answers validates a submission. It returns the answers keyed by field
// label, or a message per invalid field index.
func (s *formSpec) answers(values url.Values) (map[string]interface{}, map[int]string) {
	out := make(map[string]interface{}, len(s.Fields))
	errs := map[int]string{}
	for i, f := range s.Fields {
		vals := values[formInputName(i)]
		first := ""
		if len(vals) > 0 {
			first = strings.TrimSpace(vals[0])
		}
		if f.Type == "checkbox" && len(f.Options) > 0 {
			var picked []string
			for _, v := range vals {
				if contains(f.Options, v) {
					picked = append(picked, v)
				}
			}
			if f.Required && len(picked) == 0 {
				errs[i] = "Choose at least one option."
			}
			if picked == nil {
				picked = []string{}
			}
			out[f.Label] = picked
			continue
		}
		if f.Type == "checkbox" {
			if f.Required && first == "" {
				errs[i] = "This box must be checked."
			}
			out[f.Label] = first != ""
			continue
		}
		if first == "" {
			if f.Required {
				errs[i] = "This field is required."
			}
			out[f.Label] = ""
			continue
		}
		switch f.Type {
		case "email":
			if a, err := mail.ParseAddress(first); err != nil || a.Address != first {
				errs[i] = "Enter an email address, like name@example.com."
			}
		case "number":
			n, err := strconv.ParseFloat(first, 64)
			if err != nil {
				errs[i] = "Enter a number."
			} else {
				out[f.Label] = n
				continue
			}
		case "date":
			if _, err := time.Parse("2006-01-02", first); err != nil {
				errs[i] = "Enter a date as YYYY-MM-DD."
			}
		case "dropdown":
			if !contains(f.Options, first) {
				errs[i] = "Choose one of the options."
			}
		}
		out[f.Label] = first
	}
	return out, errs
}
