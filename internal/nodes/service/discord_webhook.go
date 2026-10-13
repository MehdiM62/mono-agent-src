package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// executeWebhook runs send_message / send_embed through a channel webhook.
// A webhook needs no bot: its URL carries its own token, so no
// Authorization header is sent.
func (n *DiscordNode) executeWebhook(ctx context.Context, operation string, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	webhookURL := strings.TrimSpace(strVal(config, "webhook_url"))
	if webhookURL == "" {
		return nil, fmt.Errorf("service.discord: 'webhook_url' is required for auth=webhook")
	}
	u, err := url.Parse(webhookURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("service.discord: 'webhook_url' must be an http(s) URL, got %q", webhookURL)
	}

	var body map[string]interface{}
	switch operation {
	case "send_message":
		text := strVal(config, "text")
		if text == "" {
			return nil, fmt.Errorf("service.discord: 'text' is required for send_message")
		}
		body = map[string]interface{}{"content": text}
	case "send_embed":
		if body, err = discordEmbedBody(config); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("service.discord: operation %q needs auth=bot (a webhook can only send_message or send_embed)", operation)
	}
	if v := strVal(config, "username"); v != "" {
		body["username"] = v
	}
	if v := strVal(config, "avatar_url"); v != "" {
		body["avatar_url"] = v
	}

	// wait=true makes Discord answer with the created message instead of 204.
	q := u.Query()
	q.Set("wait", "true")
	u.RawQuery = q.Encode()
	result, err := discordWebhookRequest(ctx, "POST", u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("service.discord %s (webhook): %w", operation, err)
	}
	result["message_id"] = result["id"]
	// A webhook always belongs to a server channel; its own record names the
	// guild, which publication history uses to tell shared from private.
	if guild, _ := result["guild_id"].(string); guild == "" {
		u.RawQuery = ""
		if hook, lookupErr := discordWebhookRequest(ctx, "GET", u.String(), nil); lookupErr == nil {
			result["guild_id"] = hook["guild_id"]
		}
	}
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{workflow.NewItem(result)}}}, nil
}

// discordEmbedBody builds a message body holding one embed from the
// embed_title / embed_description / embed_color config keys.
func discordEmbedBody(config map[string]interface{}) (map[string]interface{}, error) {
	title := strVal(config, "embed_title")
	description := strVal(config, "embed_description")
	if title == "" && description == "" {
		return nil, fmt.Errorf("service.discord: 'embed_title' or 'embed_description' is required for send_embed")
	}
	embed := map[string]interface{}{}
	if title != "" {
		embed["title"] = title
	}
	if description != "" {
		embed["description"] = description
	}
	if _, ok := config["embed_color"]; ok {
		embed["color"] = intVal(config, "embed_color")
	}
	body := map[string]interface{}{"embeds": []interface{}{embed}}
	if text := strVal(config, "text"); text != "" {
		body["content"] = text
	}
	return body, nil
}

// discordWebhookRequest performs an unauthenticated JSON request against a
// webhook URL and returns the parsed object response (empty for 204).
func discordWebhookRequest(ctx context.Context, method, endpoint string, body interface{}) (map[string]interface{}, error) {
	req, err := buildRequest(ctx, method, endpoint, "", body)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s: %w", method, err)
	}
	defer resp.Body.Close()
	respBytes, err := readBodyCapped(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, errorBodyEcho(respBytes))
	}
	result := map[string]interface{}{}
	if len(respBytes) == 0 {
		return result, nil
	}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("parsing JSON response: %w", err)
	}
	return result, nil
}
