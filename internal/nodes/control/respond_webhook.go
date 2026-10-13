package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// RespondWebhookNode answers the webhook request that started the run, for a
// trigger.webhook whose response_mode is "responseNode". It passes its input
// items through unchanged. In a run nobody is waiting on (another response
// mode, a manual run, a second Respond node) it only passes items through.
//
// Config fields:
//
//	"respond_with"     firstIncomingItem (default) | allIncomingItems | json | text | noData
//	"response_code"    HTTP status, default 200
//	"response_headers" JSON object of header name -> value
//	"response_body"    the body for json (JSON text, or an object) and text
type RespondWebhookNode struct{}

func (n *RespondWebhookNode) Type() string { return "core.respond_webhook" }

func (n *RespondWebhookNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	resp, err := buildWebhookResponse(input.Items, config)
	if err != nil {
		return nil, err
	}
	workflow.DeliverWebhookResponse(input.ExecutionID, resp)
	return []workflow.NodeOutput{{Handle: "main", Items: input.Items}}, nil
}

func buildWebhookResponse(items []workflow.Item, config map[string]interface{}) (workflow.WebhookResponse, error) {
	resp := workflow.WebhookResponse{StatusCode: 200, Headers: map[string]string{}}

	code, err := respondNumber(config["response_code"])
	if err != nil {
		return resp, err
	}
	if code != 0 {
		if code < 100 || code > 599 {
			return resp, fmt.Errorf("%w: response_code %d is not an HTTP status", workflow.ErrInvalidConfig, code)
		}
		resp.StatusCode = code
	}

	contentType := "application/json"
	respondWith, _ := config["respond_with"].(string)
	switch respondWith {
	case "", "firstIncomingItem":
		var first interface{} = map[string]interface{}{}
		if len(items) > 0 && items[0].JSON != nil {
			first = items[0].JSON
		}
		resp.Body, err = json.Marshal(first)
	case "allIncomingItems":
		all := make([]interface{}, 0, len(items))
		for _, it := range items {
			all = append(all, it.JSON)
		}
		resp.Body, err = json.Marshal(all)
	case "json":
		resp.Body, err = respondJSONBody(config["response_body"])
	case "text":
		contentType = "text/plain; charset=utf-8"
		resp.Body = []byte(fmt.Sprint(valueOr(config["response_body"], "")))
	case "noData":
		contentType = ""
	default:
		return resp, fmt.Errorf("%w: respond_with %q is not one of firstIncomingItem, allIncomingItems, json, text, noData", workflow.ErrInvalidConfig, respondWith)
	}
	if err != nil {
		return resp, err
	}
	if contentType != "" {
		resp.Headers["Content-Type"] = contentType
	}

	headers, err := respondHeaders(config["response_headers"])
	if err != nil {
		return resp, err
	}
	for k, v := range headers {
		resp.Headers[k] = v
	}
	return resp, nil
}

// respondJSONBody is the json body: JSON text is checked and sent as is, an
// object or list (an expression's value) is encoded.
func respondJSONBody(v interface{}) ([]byte, error) {
	switch b := v.(type) {
	case nil:
		return []byte("{}"), nil
	case string:
		if strings.TrimSpace(b) == "" {
			return []byte("{}"), nil
		}
		if !json.Valid([]byte(b)) {
			return nil, fmt.Errorf("%w: response_body is not valid JSON", workflow.ErrInvalidConfig)
		}
		return []byte(b), nil
	default:
		out, err := json.Marshal(b)
		if err != nil {
			return nil, fmt.Errorf("%w: response_body: %v", workflow.ErrInvalidConfig, err)
		}
		return out, nil
	}
}

// respondHeaders reads response_headers: a JSON object (text, or an object)
// of header name -> value.
func respondHeaders(v interface{}) (map[string]string, error) {
	var m map[string]interface{}
	switch h := v.(type) {
	case nil:
		return nil, nil
	case string:
		if strings.TrimSpace(h) == "" {
			return nil, nil
		}
		if err := json.Unmarshal([]byte(h), &m); err != nil {
			return nil, fmt.Errorf("%w: response_headers must be a JSON object: %v", workflow.ErrInvalidConfig, err)
		}
	case map[string]interface{}:
		m = h
	default:
		return nil, fmt.Errorf("%w: response_headers must be a JSON object", workflow.ErrInvalidConfig)
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		out[k] = fmt.Sprint(val)
	}
	return out, nil
}

func respondNumber(v interface{}) (int, error) {
	switch n := v.(type) {
	case nil:
		return 0, nil
	case float64:
		return int(n), nil
	case int:
		return n, nil
	case string:
		if strings.TrimSpace(n) == "" {
			return 0, nil
		}
		i, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return 0, fmt.Errorf("%w: response_code %q is not a number", workflow.ErrInvalidConfig, n)
		}
		return i, nil
	default:
		return 0, fmt.Errorf("%w: response_code must be a number", workflow.ErrInvalidConfig)
	}
}

func valueOr(v, def interface{}) interface{} {
	if v == nil {
		return def
	}
	return v
}
