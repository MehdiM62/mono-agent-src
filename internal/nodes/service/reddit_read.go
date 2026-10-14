package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// listComments returns the top-level comments of one post, one item each
// with id, author, body and score.
func (n *RedditNode) listComments(ctx context.Context, accessToken, userAgent string, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	thingID := strVal(config, "thing_id")
	if thingID == "" {
		return nil, fmt.Errorf("service.reddit: 'thing_id' is required for list_comments")
	}
	postID := strings.TrimPrefix(thingID, "t3_")
	endpoint := fmt.Sprintf("%s/comments/%s?raw_json=1", redditBaseURL, url.PathEscape(postID))
	raw, err := redditGetRaw(ctx, endpoint, accessToken, userAgent)
	if err != nil {
		return nil, fmt.Errorf("service.reddit list_comments: %w", err)
	}
	items, err := redditParseComments(raw)
	if err != nil {
		return nil, fmt.Errorf("service.reddit list_comments: %w", err)
	}
	return []workflow.NodeOutput{{Handle: "main", Items: items}}, nil
}

// getPostMetrics returns a post's score, comment count and permalink.
func (n *RedditNode) getPostMetrics(ctx context.Context, accessToken, userAgent string, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	thingID := strVal(config, "thing_id")
	if thingID == "" {
		return nil, fmt.Errorf("service.reddit: 'thing_id' is required for get_post_metrics")
	}
	endpoint := redditBaseURL + "/api/info?id=" + url.QueryEscape(redditEnsureFullname(thingID, "t3"))
	result, err := redditRequest(ctx, "GET", endpoint, accessToken, userAgent, nil)
	if err != nil {
		return nil, fmt.Errorf("service.reddit get_post_metrics: %w", err)
	}
	data, _ := result["data"].(map[string]interface{})
	children, _ := data["children"].([]interface{})
	if len(children) == 0 {
		return nil, fmt.Errorf("service.reddit get_post_metrics: no post found for thing_id %q", thingID)
	}
	child, _ := children[0].(map[string]interface{})
	post, _ := child["data"].(map[string]interface{})
	metrics := map[string]interface{}{
		"score":        post["score"],
		"num_comments": post["num_comments"],
		"permalink":    post["permalink"],
	}
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{workflow.NewItem(metrics)}}}, nil
}

// redditEnsureFullname prefixes a bare Reddit ID with the given type prefix
// ("t3" for posts, "t1" for comments) unless it already is a fullname.
func redditEnsureFullname(id, prefix string) string {
	if strings.Contains(id, "_") {
		return id
	}
	return prefix + "_" + id
}

// redditGetRaw performs an authenticated GET and returns the raw body (the
// comments endpoint answers with a JSON array, not an object).
func redditGetRaw(ctx context.Context, endpoint, accessToken, userAgent string) ([]byte, error) {
	req, err := buildRequest(ctx, "GET", endpoint, accessToken, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	return redditDo(req, "GET", endpoint)
}

// redditParseComments flattens the comment listing of a /comments/{id}
// response — a two-element array [post listing, comment listing] — into one
// item per top-level comment.
func redditParseComments(raw []byte) ([]workflow.Item, error) {
	var pair []struct {
		Data struct {
			Children []struct {
				Kind string                 `json:"kind"`
				Data map[string]interface{} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &pair); err != nil {
		return nil, fmt.Errorf("parsing comments response: %w", err)
	}
	if len(pair) < 2 {
		return []workflow.Item{}, nil
	}
	items := make([]workflow.Item, 0, len(pair[1].Data.Children))
	for _, child := range pair[1].Data.Children {
		if child.Kind == "more" {
			continue // a "load more comments" stub, not a comment
		}
		id, _ := child.Data["id"].(string)
		author, _ := child.Data["author"].(string)
		body, _ := child.Data["body"].(string)
		score := 0
		if s, ok := child.Data["score"].(float64); ok {
			score = int(s)
		}
		items = append(items, workflow.NewItem(map[string]interface{}{
			"id": id, "author": author, "body": body, "score": score,
		}))
	}
	return items, nil
}
