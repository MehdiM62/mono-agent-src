package workflow

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SendAndWaitSpec reads n8n's sendAndWait parameters from a node's config:
//
//	"subject", "message"   the reply page's title and text
//	"responseType"         approval (default), freeText or customForm
//	"approvalOptions"      {values: {approvalType single|double,
//	                       approveLabel, disapproveLabel}}
//	"formFields"           customForm: {values: [...]} or a list (see
//	                       ParseReplyFormFields); "defineForm": "json" reads
//	                       the list from "jsonOutput" instead
//	"options"              {limitWaitTime: {values: {...}} (ParseReplyLimit),
//	                       messageButtonLabel, responseFormTitle,
//	                       responseFormDescription, responseFormButtonLabel}
func SendAndWaitSpec(config map[string]interface{}, now time.Time) (WaitReplySpec, error) {
	spec := WaitReplySpec{Kind: WaitReplyApproval}
	if v, _ := config["responseType"].(string); v != "" {
		spec.Kind = v
	}
	spec.Title, _ = config["subject"].(string)
	spec.Message, _ = config["message"].(string)
	opts, _ := config["options"].(map[string]interface{})
	switch spec.Kind {
	case WaitReplyApproval:
		if ao, ok := config["approvalOptions"].(map[string]interface{}); ok {
			v, _ := ao["values"].(map[string]interface{})
			spec.ApprovalType, _ = v["approvalType"].(string)
			spec.ApproveLabel, _ = v["approveLabel"].(string)
			spec.DisapproveLabel, _ = v["disapproveLabel"].(string)
		}
	case WaitReplyFreeText, WaitReplyCustomForm:
		spec.LinkLabel, _ = opts["messageButtonLabel"].(string)
		if t, _ := opts["responseFormTitle"].(string); t != "" {
			spec.Title = t
		}
		if d, _ := opts["responseFormDescription"].(string); d != "" {
			spec.Message = d
		}
		spec.ButtonLabel, _ = opts["responseFormButtonLabel"].(string)
		if spec.Kind == WaitReplyCustomForm {
			raw := config["formFields"]
			if d, _ := config["defineForm"].(string); d == "json" {
				raw = config["jsonOutput"]
			}
			fields, err := ParseReplyFormFields(raw)
			if err != nil {
				return spec, err
			}
			spec.FormFields = fields
		}
	default:
		return spec, fmt.Errorf("%w: \"responseType\" must be approval, freeText or customForm, got %q", ErrInvalidConfig, spec.Kind)
	}
	if lim, ok := opts["limitWaitTime"].(map[string]interface{}); ok {
		values, _ := lim["values"].(map[string]interface{})
		if values == nil {
			values = lim
		}
		t, err := ParseReplyLimit(values, now)
		if err != nil {
			return spec, err
		}
		spec.Limit = t
	}
	return spec, nil
}

// ParseReplyLimit reads n8n's wait limit: "limitType" afterTimeInterval
// (default; "resumeAmount" default 1 × "resumeUnit" seconds|minutes|hours|
// days, default hours) or atSpecifiedTime ("maxDateAndTime").
func ParseReplyLimit(values map[string]interface{}, now time.Time) (time.Time, error) {
	switch limitType, _ := values["limitType"].(string); limitType {
	case "", "afterTimeInterval":
		amount := 1.0
		if v, ok := values["resumeAmount"]; ok && v != nil {
			f, ok := replyNumber(v)
			if !ok || f < 0 {
				return time.Time{}, fmt.Errorf("%w: \"resumeAmount\" must be a number of at least 0", ErrInvalidConfig)
			}
			amount = f
		}
		unit, _ := values["resumeUnit"].(string)
		per, ok := map[string]time.Duration{
			"": time.Hour, "seconds": time.Second, "minutes": time.Minute, "hours": time.Hour, "days": 24 * time.Hour,
		}[unit]
		if !ok {
			return time.Time{}, fmt.Errorf("%w: \"resumeUnit\" must be seconds, minutes, hours or days, got %q", ErrInvalidConfig, unit)
		}
		return now.Add(time.Duration(amount * float64(per))), nil
	case "atSpecifiedTime":
		s, _ := values["maxDateAndTime"].(string)
		t, ok := ParseDateTime(s)
		if !ok {
			return time.Time{}, fmt.Errorf("%w: \"maxDateAndTime\" must be a date and time, got %q", ErrInvalidConfig, s)
		}
		return t, nil
	default:
		return time.Time{}, fmt.Errorf("%w: \"limitType\" must be afterTimeInterval or atSpecifiedTime, got %q", ErrInvalidConfig, limitType)
	}
}

// ParseDateTime accepts RFC 3339 or a local date/time without zone
// ("2006-01-02T15:04[:05]", "2006-01-02 15:04[:05]", "2006-01-02").
func ParseDateTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func replyNumber(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	}
	return 0, false
}

// ParseReplyFormFields reads n8n-style form fields: a list, {values: list},
// or either as JSON text, of {fieldLabel, fieldName, fieldType,
// requiredField, placeholder, fieldOptions: {values: [{option}]},
// multiselect}.
func ParseReplyFormFields(raw interface{}) ([]WaitReplyField, error) {
	if s, ok := raw.(string); ok {
		if strings.TrimSpace(s) == "" {
			return nil, nil
		}
		if err := json.Unmarshal([]byte(s), &raw); err != nil {
			return nil, fmt.Errorf("%w: \"formFields\" is not JSON: %v", ErrInvalidConfig, err)
		}
	}
	if m, ok := raw.(map[string]interface{}); ok {
		raw = m["values"]
	}
	list, _ := raw.([]interface{})
	var out []WaitReplyField
	for i, v := range list {
		m, ok := v.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%w: \"formFields\"[%d] must be an object", ErrInvalidConfig, i)
		}
		f := WaitReplyField{}
		f.Label, _ = m["fieldLabel"].(string)
		f.Name, _ = m["fieldName"].(string)
		f.Type, _ = m["fieldType"].(string)
		f.Required, _ = m["requiredField"].(bool)
		f.Placeholder, _ = m["placeholder"].(string)
		f.Multiple, _ = m["multiselect"].(bool)
		if f.Label == "" && f.Name == "" {
			return nil, fmt.Errorf("%w: \"formFields\"[%d] needs a fieldLabel", ErrInvalidConfig, i)
		}
		if f.Label == "" {
			f.Label = f.Name
		}
		if opts, ok := m["fieldOptions"].(map[string]interface{}); ok {
			vals, _ := opts["values"].([]interface{})
			for _, o := range vals {
				if om, ok := o.(map[string]interface{}); ok {
					if s, ok := om["option"].(string); ok {
						f.Options = append(f.Options, s)
					}
				}
			}
		}
		out = append(out, f)
	}
	return out, nil
}
