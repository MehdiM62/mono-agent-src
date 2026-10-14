package connections

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// validateGitHub validates a GitHub connection using the token or access_token field.
func validateGitHub(ctx context.Context, c *Connection) (string, error) {
	token := getStr(c.Data, "token")
	if token == "" {
		token = getStr(c.Data, "access_token")
	}
	if token == "" {
		return "", fmt.Errorf("validateGitHub: missing token or access_token")
	}
	body, status, err := doGET(ctx, "https://api.github.com/user", "Bearer "+token)
	if err != nil {
		return "", fmt.Errorf("validateGitHub: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("validateGitHub: %w", &StatusError{Code: status})
	}
	var resp struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("validateGitHub: parse response: %w", err)
	}
	if resp.Login == "" {
		return "", fmt.Errorf("validateGitHub: empty login in response")
	}
	return resp.Login, nil
}

// validateNotion validates a Notion connection using the token or access_token field.
func validateNotion(ctx context.Context, c *Connection) (string, error) {
	token := getStr(c.Data, "token")
	if token == "" {
		token = getStr(c.Data, "access_token")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.notion.com/v1/users/me", nil)
	if err != nil {
		return "", fmt.Errorf("validateNotion: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", "2022-06-28")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("validateNotion: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("validateNotion: read body: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("validateNotion: %w", &StatusError{Code: resp.StatusCode})
	}

	var r struct {
		Name string `json:"name"`
		Bot  struct {
			WorkspaceName string `json:"workspace_name"`
		} `json:"bot"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("validateNotion: parse response: %w", err)
	}

	if r.Bot.WorkspaceName != "" {
		return r.Bot.WorkspaceName, nil
	}
	if r.Name != "" {
		return r.Name, nil
	}
	return "", fmt.Errorf("validateNotion: could not resolve account name from response")
}

// validateAirtable validates an Airtable connection using the api_key or access_token field.
func validateAirtable(ctx context.Context, c *Connection) (string, error) {
	token := getStr(c.Data, "api_key")
	if token == "" {
		token = getStr(c.Data, "access_token")
	}
	body, status, err := doGET(ctx, "https://api.airtable.com/v0/meta/whoami", "Bearer "+token)
	if err != nil {
		return "", fmt.Errorf("validateAirtable: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("validateAirtable: %w", &StatusError{Code: status})
	}
	var r struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	_ = json.Unmarshal(body, &r)
	if r.Email != "" {
		return r.Email, nil
	}
	return r.ID, nil
}

// validateJira validates a Jira connection using email, api_token, and domain fields.
func validateJira(ctx context.Context, c *Connection) (string, error) {
	email := getStr(c.Data, "email")
	apiToken := getStr(c.Data, "api_token")
	domain := getStr(c.Data, "domain")

	if email == "" {
		return "", fmt.Errorf("validateJira: missing email")
	}
	if apiToken == "" {
		return "", fmt.Errorf("validateJira: missing api_token")
	}
	if domain == "" {
		return "", fmt.Errorf("validateJira: missing domain")
	}

	url := fmt.Sprintf("https://%s/rest/api/3/myself", domain)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("validateJira: create request: %w", err)
	}
	req.SetBasicAuth(email, apiToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("validateJira: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("validateJira: read body: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("validateJira: %w", &StatusError{Code: resp.StatusCode})
	}

	var result struct {
		DisplayName string `json:"displayName"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("validateJira: parse response: %w", err)
	}
	return result.DisplayName, nil
}

// linearAuthHeader returns the Authorization header value for a Linear
// connection: personal API keys are sent as-is, OAuth access tokens need
// the standard "Bearer " prefix — sending either the wrong way
// authenticates neither. Returns "" if neither field is present.
// https://linear.app/developers/oauth-2-0-authentication
func linearAuthHeader(data map[string]interface{}) string {
	if apiKey := getStr(data, "api_key"); apiKey != "" {
		return apiKey
	}
	if token := getStr(data, "access_token"); token != "" {
		return "Bearer " + token
	}
	return ""
}

// validateLinear validates a Linear connection using the api_key or access_token field.
func validateLinear(ctx context.Context, c *Connection) (string, error) {
	authHeader := linearAuthHeader(c.Data)

	bodyStr := `{"query":"{ viewer { name email } }"}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.linear.app/graphql", strings.NewReader(bodyStr))
	if err != nil {
		return "", fmt.Errorf("validateLinear: create request: %w", err)
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("validateLinear: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("validateLinear: read body: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("validateLinear: %w", &StatusError{Code: resp.StatusCode})
	}

	var r struct {
		Data struct {
			Viewer struct {
				Email string `json:"email"`
				Name  string `json:"name"`
			} `json:"viewer"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("validateLinear: parse response: %w", err)
	}
	if r.Data.Viewer.Email != "" {
		return r.Data.Viewer.Email, nil
	}
	if r.Data.Viewer.Name != "" {
		return r.Data.Viewer.Name, nil
	}
	return "", fmt.Errorf("validateLinear: viewer has no email or name")
}

// validateStripe validates a Stripe connection using the secret_key field.
func validateStripe(ctx context.Context, c *Connection) (string, error) {
	key := getStr(c.Data, "secret_key")
	if key == "" {
		return "", fmt.Errorf("validateStripe: missing secret_key")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.stripe.com/v1/account", nil)
	if err != nil {
		return "", fmt.Errorf("validateStripe: create request: %w", err)
	}
	req.SetBasicAuth(key, "")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("validateStripe: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("validateStripe: read body: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("validateStripe: %w", &StatusError{Code: resp.StatusCode})
	}

	var result struct {
		BusinessProfile struct {
			Name string `json:"name"`
		} `json:"business_profile"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("validateStripe: parse response: %w", err)
	}
	if result.BusinessProfile.Name != "" {
		return result.BusinessProfile.Name, nil
	}
	return result.Email, nil
}

// validateHubSpot validates a HubSpot OAuth connection.
func validateHubSpot(ctx context.Context, c *Connection) (string, error) {
	token := getStr(c.Data, "access_token")
	if token == "" {
		return "", fmt.Errorf("validateHubSpot: missing access_token")
	}
	body, status, err := doGET(ctx, "https://api.hubapi.com/oauth/v1/access-tokens/"+token, "")
	if err != nil {
		return "", fmt.Errorf("validateHubSpot: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("validateHubSpot: %w", &StatusError{Code: status})
	}
	var r struct {
		User  string `json:"user"`
		HubID int    `json:"hub_id"`
	}
	_ = json.Unmarshal(body, &r)
	if r.User != "" {
		return r.User, nil
	}
	return fmt.Sprintf("hub-%d", r.HubID), nil
}

// validateSalesforce validates a Salesforce OAuth connection.
func validateSalesforce(ctx context.Context, c *Connection) (string, error) {
	token := getStr(c.Data, "access_token")
	instanceURL := getStr(c.Data, "instance_url")
	if token == "" {
		return "", fmt.Errorf("validateSalesforce: missing access_token")
	}
	if instanceURL == "" {
		instanceURL = "https://login.salesforce.com"
	}
	body, status, err := doGET(ctx, instanceURL+"/services/oauth2/userinfo", "Bearer "+token)
	if err != nil {
		return "", fmt.Errorf("validateSalesforce: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("validateSalesforce: %w", &StatusError{Code: status})
	}
	var r struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	_ = json.Unmarshal(body, &r)
	if r.Email != "" {
		return r.Email, nil
	}
	return r.Name, nil
}
