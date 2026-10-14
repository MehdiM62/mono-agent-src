package workflow

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// applyWebhookOptions reads a trigger.webhook node's response, size and dedupe
// options (see WebhookTriggerSchema) into reg.
func applyWebhookOptions(reg *WebhookRegistration, config map[string]interface{}) error {
	mode, _ := config["response_mode"].(string)
	switch mode {
	case "", WebhookRespondOnReceived, WebhookRespondLastNode, WebhookRespondNode:
		reg.ResponseMode = mode
	default:
		return fmt.Errorf("trigger.webhook: response_mode %q is not one of onReceived, lastNode, responseNode", mode)
	}
	data, _ := config["response_data"].(string)
	switch data {
	case "", webhookDataFirstEntry, webhookDataAllEntries, webhookDataNone:
		reg.ResponseData = data
	default:
		return fmt.Errorf("trigger.webhook: response_data %q is not one of firstEntryJson, allEntries, noData", data)
	}

	code, err := configNumber(config, "response_code")
	if err != nil {
		return err
	}
	if code != 0 && (code < 200 || code > 299) {
		return fmt.Errorf("trigger.webhook: response_code %v must be a 2xx status", code)
	}
	reg.ResponseCode = int(code)

	secs, err := configNumber(config, "response_timeout")
	if err != nil {
		return err
	}
	if secs < 0 {
		return fmt.Errorf("trigger.webhook: response_timeout must not be negative")
	}
	reg.ResponseTimeout = min(time.Duration(secs*float64(time.Second)), maxWebhookResponseTimeout)

	maxBody, err := configNumber(config, "max_body_bytes")
	if err != nil {
		return err
	}
	if maxBody < 0 {
		return fmt.Errorf("trigger.webhook: max_body_bytes must not be negative")
	}
	reg.MaxBodyBytes = min(int64(maxBody), maxWebhookMaxBody)

	header, _ := config["dedupe_header"].(string)
	reg.DedupeHeader = strings.TrimSpace(header)
	reg.VerifyToken, _ = config["verify_token"].(string)
	return nil
}

// configNumber reads a numeric option that may arrive as a JSON number or as
// a string (form inputs). Missing or empty is 0.
func configNumber(config map[string]interface{}, key string) (float64, error) {
	switch v := config[key].(type) {
	case nil:
		return 0, nil
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		if strings.TrimSpace(v) == "" {
			return 0, nil
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, fmt.Errorf("trigger.webhook: %s %q is not a number", key, v)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("trigger.webhook: %s must be a number", key)
	}
}
