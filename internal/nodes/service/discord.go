package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/monoes/mono-agent/internal/apipkg"
	"github.com/monoes/mono-agent/internal/workflow"
)

// DiscordNode sends messages and embeds, reads messages, adds reactions, and
// lists channels via the Discord API, authenticated either as a bot or
// through a channel webhook.
// Type: "service.discord"
//
// Config fields:
//
//	"auth"              (string, optional): "bot" (default) | "webhook"
//	"operation"         (string, optional, default "send_message"):
//	                    bot: "send_message" | "send_embed" | "list_messages" | "add_reaction" | "list_channels"
//	                    webhook: "send_message" | "send_embed"
//	"bot_token"         (string, required for auth=bot): sent as "Authorization: Bot <token>";
//	                    "token" is accepted too and a leading "Bot " is stripped
//	"webhook_url"       (string, required for auth=webhook): https://discord.com/api/webhooks/{id}/{token}
//	"username"          (string, optional, webhook): overrides the webhook's display name
//	"avatar_url"        (string, optional, webhook): overrides the webhook's avatar
//	"channel_id"        (string, required for bot send_message, send_embed, list_messages, add_reaction)
//	"text"              (string, required for send_message): message content ("content" is accepted too)
//	"embed_title"       (string, send_embed): embed title
//	"embed_description" (string, send_embed): embed description
//	"embed_color"       (int, send_embed): embed color as a decimal integer
//	"limit"             (int, optional, default 50): list_messages page size
//	"message_id"        (string, required for add_reaction)
//	"emoji"             (string, required for add_reaction)
//	"guild_id"          (string, required for list_channels)
//
// It is also the runtime for the legacy "comm.discord" type (registered as an
// alias): its "token"/"content" keys, its default send_message operation and
// its "get_channels" operation (run as list_channels) keep working, and send
// results keep their "message_id" field.
type DiscordNode struct{}

func (n *DiscordNode) Type() string { return "service.discord" }

// discordBaseURL is a var (not const) so tests can point it at an httptest server.
var discordBaseURL = "https://discord.com/api/v10"

func (n *DiscordNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	if extensionOwns("service.discord", config) {
		return apipkg.ExecuteExtension(ctx, "service.discord", input, config)
	}
	config = withFallback(config, "text", "content")
	operation := workflow.ConfigOperation(config)
	if operation == "" {
		operation = "send_message"
	}

	switch auth := strVal(config, "auth"); auth {
	case "webhook":
		return n.executeWebhook(ctx, operation, config)
	case "", "bot":
	default:
		return nil, fmt.Errorf("service.discord: unknown auth %q (want bot or webhook)", auth)
	}

	botToken := strings.TrimPrefix(strVal(withFallback(config, "bot_token", "token"), "bot_token"), "Bot ")
	if botToken == "" {
		return nil, fmt.Errorf("service.discord: 'bot_token' is required")
	}

	switch operation {
	case "send_message":
		return n.sendMessage(ctx, botToken, config)
	case "send_embed":
		return n.sendEmbed(ctx, botToken, config)
	case "list_messages":
		return n.listMessages(ctx, botToken, config)
	case "add_reaction":
		return n.addReaction(ctx, botToken, config)
	case "list_channels", "get_channels": // get_channels: legacy comm.discord name
		return n.listChannels(ctx, botToken, config)
	default:
		return nil, fmt.Errorf("service.discord: unknown operation %q", operation)
	}
}

func (n *DiscordNode) sendMessage(ctx context.Context, botToken string, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	channelID := strVal(config, "channel_id")
	text := strVal(config, "text")
	if channelID == "" || text == "" {
		return nil, fmt.Errorf("service.discord: 'channel_id' and 'text' are required for send_message")
	}
	return n.postToChannel(ctx, botToken, channelID, "send_message", map[string]interface{}{"content": text})
}

func (n *DiscordNode) sendEmbed(ctx context.Context, botToken string, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	channelID := strVal(config, "channel_id")
	if channelID == "" {
		return nil, fmt.Errorf("service.discord: 'channel_id' is required for send_embed")
	}
	body, err := discordEmbedBody(config)
	if err != nil {
		return nil, err
	}
	return n.postToChannel(ctx, botToken, channelID, "send_embed", body)
}

// postToChannel creates a message in a channel as the bot.
func (n *DiscordNode) postToChannel(ctx context.Context, botToken, channelID, op string, body map[string]interface{}) ([]workflow.NodeOutput, error) {
	endpoint := discordBaseURL + "/channels/" + url.PathEscape(channelID) + "/messages"
	result, err := discordRequest(ctx, "POST", endpoint, botToken, body)
	if err != nil {
		return nil, fmt.Errorf("service.discord %s: %w", op, err)
	}
	result["message_id"] = result["id"]
	// Discord message responses may omit guild_id. Resolve channel metadata
	// after success so private messages never enter publication history. A
	// metadata failure cannot turn a sent message into a retryable failure.
	if ch, lookupErr := discordRequest(ctx, "GET", discordBaseURL+"/channels/"+url.PathEscape(channelID), botToken, nil); lookupErr == nil {
		result["channel_type"] = ch["type"]
		result["guild_id"] = ch["guild_id"]
	} else if guild, _ := result["guild_id"].(string); guild == "" {
		fmt.Fprintln(os.Stderr, "Warning: publication history skipped because Discord channel visibility could not be verified; the message was sent successfully.")
	}
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{workflow.NewItem(result)}}}, nil
}

func (n *DiscordNode) listMessages(ctx context.Context, botToken string, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	channelID := strVal(config, "channel_id")
	if channelID == "" {
		return nil, fmt.Errorf("service.discord: 'channel_id' is required for list_messages")
	}
	limit := intVal(config, "limit")
	if limit <= 0 {
		limit = 50
	}

	endpoint := fmt.Sprintf("%s/channels/%s/messages?limit=%d", discordBaseURL, url.PathEscape(channelID), limit)
	results, err := discordRequestList(ctx, "GET", endpoint, botToken, nil)
	if err != nil {
		return nil, fmt.Errorf("service.discord list_messages: %w", err)
	}
	return []workflow.NodeOutput{{Handle: "main", Items: discordItemsFromList(results)}}, nil
}

func (n *DiscordNode) addReaction(ctx context.Context, botToken string, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	channelID := strVal(config, "channel_id")
	messageID := strVal(config, "message_id")
	emoji := strVal(config, "emoji")
	if channelID == "" || messageID == "" || emoji == "" {
		return nil, fmt.Errorf("service.discord: 'channel_id', 'message_id', and 'emoji' are required for add_reaction")
	}

	endpoint := discordBaseURL + "/channels/" + url.PathEscape(channelID) +
		"/messages/" + url.PathEscape(messageID) +
		"/reactions/" + url.PathEscape(emoji) + "/@me"
	result, err := discordRequest(ctx, "PUT", endpoint, botToken, nil)
	if err != nil {
		return nil, fmt.Errorf("service.discord add_reaction: %w", err)
	}
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{workflow.NewItem(result)}}}, nil
}

func (n *DiscordNode) listChannels(ctx context.Context, botToken string, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	guildID := strVal(config, "guild_id")
	if guildID == "" {
		return nil, fmt.Errorf("service.discord: 'guild_id' is required for list_channels")
	}

	endpoint := discordBaseURL + "/guilds/" + url.PathEscape(guildID) + "/channels"
	results, err := discordRequestList(ctx, "GET", endpoint, botToken, nil)
	if err != nil {
		return nil, fmt.Errorf("service.discord list_channels: %w", err)
	}
	return []workflow.NodeOutput{{Handle: "main", Items: discordItemsFromList(results)}}, nil
}

// discordItemsFromList converts a []interface{} of JSON objects into workflow.Items.
func discordItemsFromList(results []interface{}) []workflow.Item {
	items := make([]workflow.Item, 0, len(results))
	for _, r := range results {
		if m, ok := r.(map[string]interface{}); ok {
			items = append(items, workflow.NewItem(m))
		}
	}
	return items
}

// discordRequest performs an authenticated request against the Discord Bot
// API and returns a parsed JSON object response.
func discordRequest(ctx context.Context, method, endpoint, botToken string, body interface{}) (map[string]interface{}, error) {
	respBytes, err := discordDo(ctx, method, endpoint, botToken, body)
	if err != nil {
		return nil, err
	}
	if len(respBytes) == 0 {
		return map[string]interface{}{}, nil
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("parsing JSON response: %w (body: %s)", err, string(respBytes))
	}
	return result, nil
}

// discordRequestList is like discordRequest but returns []interface{} for array responses.
func discordRequestList(ctx context.Context, method, endpoint, botToken string, body interface{}) ([]interface{}, error) {
	respBytes, err := discordDo(ctx, method, endpoint, botToken, body)
	if err != nil {
		return nil, err
	}
	if len(respBytes) == 0 {
		return []interface{}{}, nil
	}
	var result []interface{}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("parsing JSON array response: %w (body: %s)", err, string(respBytes))
	}
	return result, nil
}

// discordDo builds a request via buildRequest (no bearer token), swaps in the
// "Authorization: Bot <token>" header Discord expects (not "Bearer"),
// executes it, and returns the raw response body for a successful (2xx)
// response.
func discordDo(ctx context.Context, method, endpoint, botToken string, body interface{}) ([]byte, error) {
	req, err := buildRequest(ctx, method, endpoint, "", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bot "+botToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s %s: %w", method, endpoint, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBytes))
	}
	return respBytes, nil
}
