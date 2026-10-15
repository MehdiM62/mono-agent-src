package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// validateSlack validates a Slack connection using the access_token field.
func validateSlack(ctx context.Context, c *Connection) (string, error) {
	token := getStr(c.Data, "access_token")
	body, status, err := doGET(ctx, "https://slack.com/api/auth.test", "Bearer "+token)
	if err != nil {
		return "", fmt.Errorf("validateSlack: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("validateSlack: %w", &StatusError{Code: status})
	}

	var resp struct {
		OK    bool   `json:"ok"`
		Team  string `json:"team"`
		User  string `json:"user"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("validateSlack: parse response: %w", err)
	}
	if !resp.OK {
		return "", fmt.Errorf("validateSlack: api error: %s", resp.Error)
	}
	return fmt.Sprintf("%s / %s", resp.Team, resp.User), nil
}

// validateDiscord validates a Discord bot connection using the bot_token field.
func validateDiscord(ctx context.Context, c *Connection) (string, error) {
	token := getStr(c.Data, "bot_token")
	body, status, err := doGET(ctx, "https://discord.com/api/v10/users/@me", "Bot "+token)
	if err != nil {
		return "", fmt.Errorf("validateDiscord: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("validateDiscord: %w", &StatusError{Code: status})
	}

	var resp struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("validateDiscord: parse response: %w", err)
	}
	return resp.Username, nil
}

// validateBluesky validates a Bluesky connection by creating a session
// with the identifier/app_password fields — AT Protocol has no long-lived
// token to independently verify, so a successful session creation IS the check.
func validateBluesky(ctx context.Context, c *Connection) (string, error) {
	identifier := getStr(c.Data, "identifier")
	password := getStr(c.Data, "app_password")

	body, err := json.Marshal(map[string]string{"identifier": identifier, "password": password})
	if err != nil {
		return "", fmt.Errorf("validateBluesky: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://bsky.social/xrpc/com.atproto.server.createSession", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("validateBluesky: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("validateBluesky: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("validateBluesky: read body: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("validateBluesky: %w", &StatusError{Code: resp.StatusCode})
	}

	var result struct {
		Handle string `json:"handle"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("validateBluesky: parse response: %w", err)
	}
	return result.Handle, nil
}

// redditUserAgent is Reddit's required custom User-Agent format:
// "platform:app_id:version (by /u/username)". Reddit aggressively
// rate-limits requests using default/generic User-Agent strings.
const redditUserAgent = "monoagent:workflow-node:1.0 (by /u/monoagent)"

// validateReddit validates a Reddit OAuth connection using the access_token field.
func validateReddit(ctx context.Context, c *Connection) (string, error) {
	token := getStr(c.Data, "access_token")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://oauth.reddit.com/api/v1/me", nil)
	if err != nil {
		return "", fmt.Errorf("validateReddit: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", redditUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("validateReddit: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("validateReddit: read body: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("validateReddit: %w", &StatusError{Code: resp.StatusCode})
	}

	var result struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("validateReddit: parse response: %w", err)
	}
	return result.Name, nil
}

// validateMastodon validates a Mastodon connection using instance_url and access_token.
func validateMastodon(ctx context.Context, c *Connection) (string, error) {
	instanceURL := strings.TrimSuffix(getStr(c.Data, "instance_url"), "/")
	token := getStr(c.Data, "access_token")
	if instanceURL == "" {
		return "", fmt.Errorf("validateMastodon: missing instance_url")
	}
	body, status, err := doGET(ctx, instanceURL+"/api/v1/accounts/verify_credentials", "Bearer "+token)
	if err != nil {
		return "", fmt.Errorf("validateMastodon: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("validateMastodon: %w", &StatusError{Code: status})
	}

	var resp struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("validateMastodon: parse response: %w", err)
	}
	return resp.Username, nil
}

// validateTwilio validates a Twilio connection using account_sid and auth_token fields.
func validateTwilio(ctx context.Context, c *Connection) (string, error) {
	accountSID := getStr(c.Data, "account_sid")
	authToken := getStr(c.Data, "auth_token")

	if accountSID == "" || authToken == "" {
		return "", fmt.Errorf("validateTwilio: missing account_sid or auth_token")
	}

	url := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s.json", accountSID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("validateTwilio: create request: %w", err)
	}
	req.SetBasicAuth(accountSID, authToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("validateTwilio: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("validateTwilio: read body: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("validateTwilio: %w", &StatusError{Code: resp.StatusCode})
	}

	var result struct {
		FriendlyName string `json:"friendly_name"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("validateTwilio: parse response: %w", err)
	}
	return result.FriendlyName, nil
}

// validateTelegram validates a Telegram bot token using the getMe endpoint.
func validateTelegram(ctx context.Context, c *Connection) (string, error) {
	token := getStr(c.Data, "bot_token")
	if token == "" {
		return "", fmt.Errorf("validateTelegram: missing bot_token")
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", token)
	body, status, err := doGET(ctx, url, "")
	if err != nil {
		return "", fmt.Errorf("validateTelegram: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("validateTelegram: invalid token: %w", &StatusError{Code: status})
	}
	var resp struct {
		OK     bool `json:"ok"`
		Result struct {
			Username  string `json:"username"`
			FirstName string `json:"first_name"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("validateTelegram: parse response: %w", err)
	}
	if !resp.OK {
		return "", fmt.Errorf("validateTelegram: token rejected by Telegram")
	}
	if resp.Result.Username != "" {
		return "@" + resp.Result.Username, nil
	}
	return resp.Result.FirstName, nil
}

// validateDevTo checks a Dev.to API key by fetching the authenticated user.
func validateDevTo(ctx context.Context, c *Connection) (string, error) {
	apiKey := getStr(c.Data, "api_key")
	if apiKey == "" {
		return "", fmt.Errorf("validateDevTo: missing api_key")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://dev.to/api/users/me", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("api-key", apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("validateDevTo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("validateDevTo: %w", &StatusError{Code: resp.StatusCode})
	}
	var r struct {
		Username string `json:"username"`
	}
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &r)
	if r.Username == "" {
		return "", fmt.Errorf("validateDevTo: could not resolve username")
	}
	return r.Username, nil
}

// validateHashnode checks a Hashnode personal access token via the GraphQL API.
func validateHashnode(ctx context.Context, c *Connection) (string, error) {
	token := getStr(c.Data, "token")
	if token == "" {
		return "", fmt.Errorf("validateHashnode: missing token")
	}
	payload := []byte(`{"query":"{ me { username } }"}`)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://gql.hashnode.com", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", token)
	// Don't follow redirects: since 2026-05-13 Hashnode 301s every GraphQL
	// request — valid token or not — to an HTML announcement page when the
	// publication isn't on a Pro plan. The default client would silently
	// follow it, fail to parse HTML as JSON, and misreport this as a bad
	// token via the "could not resolve username" case below.
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("validateHashnode: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return "", fmt.Errorf("validateHashnode: Hashnode redirected this request to %s instead of returning data — the publication likely needs a Pro plan for API access (see https://hashnode.com/changelog/2026-05-13-graphql-api-paid-access); the token was never actually checked", resp.Header.Get("Location"))
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("validateHashnode: %w", &StatusError{Code: resp.StatusCode})
	}
	var r struct {
		Data struct {
			Me struct {
				Username string `json:"username"`
			} `json:"me"`
		} `json:"data"`
	}
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &r)
	if r.Data.Me.Username == "" {
		return "", fmt.Errorf("validateHashnode: could not resolve username")
	}
	return r.Data.Me.Username, nil
}

// validateProductHunt checks a Product Hunt developer token via the GraphQL API.
func validateProductHunt(ctx context.Context, c *Connection) (string, error) {
	token := getStr(c.Data, "access_token")
	if token == "" {
		return "", fmt.Errorf("validateProductHunt: missing access_token")
	}
	payload := []byte(`{"query":"{ viewer { user { name } } }"}`)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.producthunt.com/v2/api/graphql", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("validateProductHunt: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("validateProductHunt: %w", &StatusError{Code: resp.StatusCode})
	}
	var r struct {
		Data struct {
			Viewer struct {
				User struct {
					Name string `json:"name"`
				} `json:"user"`
			} `json:"viewer"`
		} `json:"data"`
	}
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &r)
	if r.Data.Viewer.User.Name == "" {
		return "", fmt.Errorf("validateProductHunt: could not resolve viewer name")
	}
	return r.Data.Viewer.User.Name, nil
}
