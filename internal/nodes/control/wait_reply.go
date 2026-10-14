package control

import (
	"context"
	"fmt"
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
		fields, err := workflow.ParseReplyFormFields(config["formFields"])
		if err != nil {
			return spec, fmt.Errorf("wait node: %w", err)
		}
		if len(fields) == 0 {
			return spec, fmt.Errorf("%w: wait node \"formFields\" needs at least one field for resume form", workflow.ErrInvalidConfig)
		}
		spec.FormFields = fields
	}
	if on, _ := config["limitWaitTime"].(bool); on {
		limit, err := workflow.ParseReplyLimit(config, now)
		if err != nil {
			return spec, fmt.Errorf("wait node: %w", err)
		}
		spec.Limit = limit
	}
	return spec, nil
}
