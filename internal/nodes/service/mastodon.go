package service

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/monoes/mono-agent/internal/apipkg"
	"github.com/monoes/mono-agent/internal/workflow"
)

// MastodonNode publishes statuses and reads timeline/account data via the
// Mastodon REST API (v1).
// Type: "service.mastodon"
//
// Config fields:
//
//	"operation"     (string, required): "publish_status" | "get_timeline" | "get_account" | "favourite" | "boost" | "get_status_metrics"
//	"access_token"  (string, required): OAuth access token from Mastodon settings
//	"instance"      (string, optional, default mastodonDefaultInstance): Mastodon instance URL;
//	                "instance_url" (the key a saved mastodon connection stores) is read when it is empty
//	"text"          (string, required for publish_status): status text
//	"visibility"    (string, optional): public | unlisted | private | direct
//	"in_reply_to_id" (string, optional): status ID to reply to (publish_status)
//	"spoiler_text"  (string, optional): content warning text
//	"media_ids"     ([]string, optional): attached media IDs
//	"limit"         (int, optional, default 20): get_timeline page size
//	"status_id"     (string or number, required for favourite, boost, get_status_metrics): target status ID
//
// It is also the runtime for the legacy "comm.mastodon" type (registered as an
// alias): its "create_status" operation runs as publish_status with the
// visibility defaulting to "public", as it always did there.
type MastodonNode struct{}

func (n *MastodonNode) Type() string { return "service.mastodon" }

// mastodonDefaultInstance is a var (not const) so tests can point it at an httptest server.
var mastodonDefaultInstance = "https://mastodon.social"

func (n *MastodonNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	if extensionOwns("service.mastodon", config) {
		return apipkg.ExecuteExtension(ctx, "service.mastodon", input, config)
	}
	accessToken := strVal(config, "access_token")
	if accessToken == "" {
		return nil, fmt.Errorf("service.mastodon: 'access_token' is required")
	}
	operation := workflow.ConfigOperation(config)

	instance := mastodonNormalizeInstanceURL(strVal(config, "instance"))
	if instance == "" {
		instance = mastodonNormalizeInstanceURL(strVal(config, "instance_url"))
	}
	if instance == "" {
		instance = mastodonDefaultInstance
	}
	baseURL := instance + "/api/v1"

	switch operation {
	case "publish_status":
		return n.publishStatus(ctx, baseURL, accessToken, config)
	case "create_status": // legacy comm.mastodon name
		if strVal(config, "visibility") == "" {
			config = withDefault(config, "visibility", "public")
		}
		return n.publishStatus(ctx, baseURL, accessToken, config)
	case "get_status_metrics":
		return n.getStatusMetrics(ctx, baseURL, accessToken, config)
	case "get_timeline":
		return n.getTimeline(ctx, baseURL, accessToken, config)
	case "get_account":
		return n.getAccount(ctx, baseURL, accessToken)
	case "favourite":
		return n.favourite(ctx, baseURL, accessToken, config)
	case "boost":
		return n.boost(ctx, baseURL, accessToken, config)
	default:
		return nil, fmt.Errorf("service.mastodon: unknown operation %q", operation)
	}
}

func (n *MastodonNode) publishStatus(ctx context.Context, baseURL, accessToken string, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	text := strVal(config, "text")
	if text == "" {
		return nil, fmt.Errorf("service.mastodon: 'text' is required for publish_status")
	}

	body := map[string]interface{}{"status": text}
	if visibility := strVal(config, "visibility"); visibility != "" {
		body["visibility"] = visibility
	}
	if spoilerText := strVal(config, "spoiler_text"); spoilerText != "" {
		body["spoiler_text"] = spoilerText
	}
	if mediaIDs := strSliceVal(config, "media_ids"); len(mediaIDs) > 0 {
		body["media_ids"] = mediaIDs
	}
	if replyID := strVal(config, "in_reply_to_id"); replyID != "" {
		body["in_reply_to_id"] = replyID
	}

	result, err := apiRequest(ctx, "POST", baseURL+"/statuses", accessToken, body)
	if err != nil {
		return nil, fmt.Errorf("service.mastodon publish_status: %w", err)
	}
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{workflow.NewItem(result)}}}, nil
}

func (n *MastodonNode) getTimeline(ctx context.Context, baseURL, accessToken string, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	limit := intVal(config, "limit")
	if limit <= 0 {
		limit = 20
	}
	endpoint := fmt.Sprintf("%s/timelines/home?limit=%d", baseURL, limit)
	results, err := apiRequestList(ctx, "GET", endpoint, accessToken, nil)
	if err != nil {
		return nil, fmt.Errorf("service.mastodon get_timeline: %w", err)
	}
	items := mastodonItemsFromList(results)
	return []workflow.NodeOutput{{Handle: "main", Items: items}}, nil
}

func (n *MastodonNode) getAccount(ctx context.Context, baseURL, accessToken string) ([]workflow.NodeOutput, error) {
	result, err := apiRequest(ctx, "GET", baseURL+"/accounts/verify_credentials", accessToken, nil)
	if err != nil {
		return nil, fmt.Errorf("service.mastodon get_account: %w", err)
	}
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{workflow.NewItem(result)}}}, nil
}

func (n *MastodonNode) favourite(ctx context.Context, baseURL, accessToken string, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	statusID := mastodonStatusID(config)
	if statusID == "" {
		return nil, fmt.Errorf("service.mastodon: 'status_id' is required for favourite")
	}
	endpoint := fmt.Sprintf("%s/statuses/%s/favourite", baseURL, url.PathEscape(statusID))
	result, err := apiRequest(ctx, "POST", endpoint, accessToken, nil)
	if err != nil {
		return nil, fmt.Errorf("service.mastodon favourite: %w", err)
	}
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{workflow.NewItem(result)}}}, nil
}

func (n *MastodonNode) boost(ctx context.Context, baseURL, accessToken string, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	statusID := mastodonStatusID(config)
	if statusID == "" {
		return nil, fmt.Errorf("service.mastodon: 'status_id' is required for boost")
	}
	endpoint := fmt.Sprintf("%s/statuses/%s/reblog", baseURL, url.PathEscape(statusID))
	result, err := apiRequest(ctx, "POST", endpoint, accessToken, nil)
	if err != nil {
		return nil, fmt.Errorf("service.mastodon boost: %w", err)
	}
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{workflow.NewItem(result)}}}, nil
}

// getStatusMetrics reads one status and returns its id, url and engagement
// counts — the same shape the legacy comm.mastodon node returned.
func (n *MastodonNode) getStatusMetrics(ctx context.Context, baseURL, accessToken string, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	statusID := mastodonStatusID(config)
	if statusID == "" {
		return nil, fmt.Errorf("service.mastodon: 'status_id' is required for get_status_metrics")
	}
	status, err := apiRequest(ctx, "GET", baseURL+"/statuses/"+url.PathEscape(statusID), accessToken, nil)
	if err != nil {
		return nil, fmt.Errorf("service.mastodon get_status_metrics: %w", err)
	}
	result := map[string]interface{}{
		"id":               status["id"],
		"url":              status["url"],
		"favourites_count": mastodonCount(status["favourites_count"]),
		"reblogs_count":    mastodonCount(status["reblogs_count"]),
		"replies_count":    mastodonCount(status["replies_count"]),
	}
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{workflow.NewItem(result)}}}, nil
}

// mastodonCount normalises an optional count field (absent means zero) to float64.
func mastodonCount(v interface{}) float64 {
	f, _ := v.(float64)
	return f
}

// mastodonNormalizeInstanceURL trims whitespace and a trailing slash and
// adds an https:// scheme to a bare hostname ("fosstodon.org").
func mastodonNormalizeInstanceURL(u string) string {
	u = strings.TrimSuffix(strings.TrimSpace(u), "/")
	if u == "" {
		return ""
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}
	return u
}

// mastodonItemsFromList converts a []interface{} of JSON objects into workflow.Items.
func mastodonItemsFromList(results []interface{}) []workflow.Item {
	items := make([]workflow.Item, 0, len(results))
	for _, r := range results {
		if m, ok := r.(map[string]interface{}); ok {
			items = append(items, workflow.NewItem(m))
		}
	}
	return items
}

// mastodonStatusID extracts "status_id" from config as a string, accepting
// either a string or a JSON number (float64/int).
func mastodonStatusID(config map[string]interface{}) string {
	switch v := config["status_id"].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	default:
		return ""
	}
}
