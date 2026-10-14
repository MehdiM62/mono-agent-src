package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

// waitForReply is core.wait's resume "webhook" and "form" (n8n's Wait):
// the run waits, durably and without a slot, until the execution's resume
// URL ($execution.resumeUrl) is called or its form answered.
//
// Config fields (n8n's names):
//
//	"httpMethod"    webhook only: the method the call must use ("" = any)
//	"formTitle", "formDescription", "buttonLabel" form only: the page
//	"formFields"    form only: a list (or {values: [...]}, or that as JSON
//	                text) of {fieldLabel, fieldName, fieldType,
//	                requiredField, placeholder, fieldOptions: {values:
//	                [{option}]}, multiselect}
//	"limitWaitTime" true to give up: "limitType" afterTimeInterval
//	                ("resumeAmount" × "resumeUnit", default 1 hours) or
//	                atSpecifiedTime ("maxDateAndTime")
//
// Outputs: webhook — the request as trigger.webhook's item (body, query,
// headers, method); form — the answers keyed by field, with submittedAt.
// When the limit passes first, the input items continue unchanged.
func waitForReply(ctx context.Context, input workflow.NodeInput, config map[string]interface{}, resume string) ([]workflow.NodeOutput, error) {
	res, err := workflow.WaitReplyAnswer(ctx, input)
	if err != nil {
		return nil, err
	}
	if res == nil {
		spec, err := waitReplySpec(config, resume, time.Now())
		if err != nil {
			return nil, err
		}
		reply, err := workflow.NewWaitReply(ctx, input, spec)
		if err != nil {
			return nil, err
		}
		return nil, reply.Pause()
	}
	switch res.Status {
	case workflow.WaitReplyPending:
		return nil, res.Pause()
	case workflow.WaitReplyExpired:
		return []workflow.NodeOutput{{Handle: "main", Items: input.Items}}, nil
	}
	if res.Kind == workflow.WaitReplyWebhook {
		return []workflow.NodeOutput{{Handle: "main", Items: res.Items()}}, nil
	}
	out := make(map[string]interface{}, len(res.Answer))
	for k, v := range res.Answer {
		out[k] = v
	}
	out["submittedAt"] = out["respondedAt"]
	delete(out, "respondedAt")
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{{JSON: out}}}}, nil
}

// waitReplySpec builds the reply a webhook or form wait opens.
func waitReplySpec(config map[string]interface{}, resume string, now time.Time) (workflow.WaitReplySpec, error) {
	spec := workflow.WaitReplySpec{Kind: workflow.WaitReplyWebhook, ExecutionURL: true}
	if resume == "webhook" {
		spec.HTTPMethod, _ = config["httpMethod"].(string)
	} else {
		spec.Kind = workflow.WaitReplyCustomForm
		spec.Title, _ = config["formTitle"].(string)
		spec.Message, _ = config["formDescription"].(string)
		spec.ButtonLabel, _ = config["buttonLabel"].(string)
		fields, err := waitFormFields(config["formFields"])
		if err != nil {
			return spec, err
		}
		if len(fields) == 0 {
			return spec, fmt.Errorf("%w: wait node \"formFields\" needs at least one field for resume form", workflow.ErrInvalidConfig)
		}
		spec.FormFields = fields
	}
	if on, _ := config["limitWaitTime"].(bool); !on {
		return spec, nil
	}
	switch limitType, _ := config["limitType"].(string); limitType {
	case "", "afterTimeInterval":
		amount := 1.0
		if v, ok := config["resumeAmount"]; ok {
			f, err := toFloat(v)
			if err != nil || f < 0 {
				return spec, fmt.Errorf("%w: wait node \"resumeAmount\" must be a number of at least 0", workflow.ErrInvalidConfig)
			}
			amount = f
		}
		unit, _ := config["resumeUnit"].(string)
		per, ok := map[string]time.Duration{
			"": time.Hour, "seconds": time.Second, "minutes": time.Minute, "hours": time.Hour, "days": 24 * time.Hour,
		}[unit]
		if !ok {
			return spec, fmt.Errorf("%w: wait node \"resumeUnit\" must be seconds, minutes, hours or days, got %q", workflow.ErrInvalidConfig, unit)
		}
		spec.Limit = now.Add(time.Duration(amount * float64(per)))
	case "atSpecifiedTime":
		s, _ := config["maxDateAndTime"].(string)
		t, ok := parseWaitDateTime(s)
		if !ok {
			return spec, fmt.Errorf("%w: wait node \"maxDateAndTime\" must be a date and time, got %q", workflow.ErrInvalidConfig, s)
		}
		spec.Limit = t
	default:
		return spec, fmt.Errorf("%w: wait node \"limitType\" must be afterTimeInterval or atSpecifiedTime, got %q", workflow.ErrInvalidConfig, limitType)
	}
	return spec, nil
}

// waitFormFields reads n8n-style form fields.
func waitFormFields(raw interface{}) ([]workflow.WaitReplyField, error) {
	if s, ok := raw.(string); ok {
		if strings.TrimSpace(s) == "" {
			return nil, nil
		}
		if err := json.Unmarshal([]byte(s), &raw); err != nil {
			return nil, fmt.Errorf("%w: wait node \"formFields\" is not JSON: %v", workflow.ErrInvalidConfig, err)
		}
	}
	if m, ok := raw.(map[string]interface{}); ok {
		raw = m["values"]
	}
	list, _ := raw.([]interface{})
	var out []workflow.WaitReplyField
	for i, v := range list {
		m, ok := v.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%w: wait node \"formFields\"[%d] must be an object", workflow.ErrInvalidConfig, i)
		}
		f := workflow.WaitReplyField{}
		f.Label, _ = m["fieldLabel"].(string)
		f.Name, _ = m["fieldName"].(string)
		f.Type, _ = m["fieldType"].(string)
		f.Required, _ = m["requiredField"].(bool)
		f.Placeholder, _ = m["placeholder"].(string)
		f.Multiple, _ = m["multiselect"].(bool)
		if f.Label == "" && f.Name == "" {
			return nil, fmt.Errorf("%w: wait node \"formFields\"[%d] needs a fieldLabel", workflow.ErrInvalidConfig, i)
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
