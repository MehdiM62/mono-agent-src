package connections

import (
	"context"
	"encoding/json"
	"fmt"
)

// validateGoogle validates a Google OAuth connection (Sheets, Drive, Gmail)
// by calling the Drive about endpoint (works with drive.readonly / spreadsheets scopes)
// or the Gmail profile endpoint depending on the platform.
func validateGoogle(ctx context.Context, c *Connection) (string, error) {
	token := getStr(c.Data, "access_token")
	if token == "" {
		return "", fmt.Errorf("validateGoogle: missing access_token")
	}

	// Try Gmail profile endpoint first for gmail platform
	if c.Platform == "gmail" {
		body, status, err := doGET(ctx, "https://gmail.googleapis.com/gmail/v1/users/me/profile", "Bearer "+token)
		if err != nil {
			return "", fmt.Errorf("validateGoogle: %w", err)
		}
		if status == 401 {
			return "", fmt.Errorf("validateGoogle: token expired or invalid: %w", &StatusError{Code: status})
		}
		if status == 200 {
			var r struct {
				EmailAddress string `json:"emailAddress"`
			}
			_ = json.Unmarshal(body, &r)
			if r.EmailAddress != "" {
				return r.EmailAddress, nil
			}
		}
	}

	// For Sheets/Drive (or Gmail fallback): use Drive about endpoint
	body, status, err := doGET(ctx, "https://www.googleapis.com/drive/v3/about?fields=user", "Bearer "+token)
	if err != nil {
		return "", fmt.Errorf("validateGoogle: %w", err)
	}
	if status == 401 {
		return "", fmt.Errorf("validateGoogle: token expired or invalid (HTTP 401)")
	}
	if status != 200 {
		return "", fmt.Errorf("validateGoogle: %w", &StatusError{Code: status, Body: string(body)})
	}
	var r struct {
		User struct {
			DisplayName  string `json:"displayName"`
			EmailAddress string `json:"emailAddress"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("validateGoogle: parse response: %w", err)
	}
	if r.User.EmailAddress != "" {
		return r.User.EmailAddress, nil
	}
	if r.User.DisplayName != "" {
		return r.User.DisplayName, nil
	}
	return "", nil
}

// validateYouTube validates a YouTube OAuth connection using the access_token field.
func validateYouTube(ctx context.Context, c *Connection) (string, error) {
	token := getStr(c.Data, "access_token")
	body, status, err := doGET(ctx, "https://www.googleapis.com/youtube/v3/channels?part=snippet&mine=true", "Bearer "+token)
	if err != nil {
		return "", fmt.Errorf("validateYouTube: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("validateYouTube: %w", &StatusError{Code: status})
	}

	var resp struct {
		Items []struct {
			Snippet struct {
				Title string `json:"title"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("validateYouTube: parse response: %w", err)
	}
	if len(resp.Items) == 0 {
		return "", fmt.Errorf("validateYouTube: no channel found for this account")
	}
	return resp.Items[0].Snippet.Title, nil
}

// validateOutlookOAuth validates an Outlook OAuth connection via Microsoft Graph.
func validateOutlookOAuth(ctx context.Context, c *Connection) (string, error) {
	token := getStr(c.Data, "access_token")
	if token == "" {
		return "", fmt.Errorf("validateOutlookOAuth: missing access_token")
	}
	// Resolves the real mailbox address (not just a liveness check) so
	// connect/refresh/test all keep AccountID/Label showing which account is
	// actually behind this connection — a profile name is a human label,
	// not a guarantee of which mailbox got authenticated. /me itself needs
	// a User.Read scope this app never requests (only Mail.ReadWrite/
	// Mail.Send/offline_access), so the address is read from the smallest
	// possible mail data instead: one field of at most one message.
	address, _, err := outlookWhoAmI(ctx, token)
	if err != nil {
		return "", fmt.Errorf("validateOutlookOAuth: %w", err)
	}
	return address, nil
}

// outlookWhoAmI resolves the mailbox owner's own address using the smallest
// possible read: one field ("from" or "toRecipients") of at most one
// message, no subject/body/content fetched.
func outlookWhoAmI(ctx context.Context, token string) (address, source string, err error) {
	// A Sent Items message's "from" is always the account owner.
	body, status, err := doGET(ctx, "https://graph.microsoft.com/v1.0/me/mailFolders/sentitems/messages?$top=1&$select=from", "Bearer "+token)
	if err == nil && status == 200 {
		if addr := firstEmailAddressField(body, "from"); addr != "" {
			return addr, "sentitems", nil
		}
	}
	// Fall back to Inbox: mail addressed to the owner names them in toRecipients.
	body, status, err = doGET(ctx, "https://graph.microsoft.com/v1.0/me/mailFolders/inbox/messages?$top=1&$select=toRecipients", "Bearer "+token)
	if err != nil {
		return "", "", err
	}
	if status != 200 {
		return "", "", &StatusError{Code: status, Body: string(body)}
	}
	if addr := firstEmailAddressField(body, "toRecipients"); addr != "" {
		return addr, "inbox", nil
	}
	return "", "", fmt.Errorf("mailbox has no sent or received messages to resolve identity from")
}

// firstEmailAddressField extracts the address from the first message's given
// field in a raw Graph list response, which is either a single
// {"emailAddress":{...}} object ("from") or an array of them ("toRecipients").
func firstEmailAddressField(body []byte, field string) string {
	var parsed struct {
		Value []map[string]interface{} `json:"value"`
	}
	if json.Unmarshal(body, &parsed) != nil || len(parsed.Value) == 0 {
		return ""
	}
	msg := parsed.Value[0]
	switch field {
	case "from":
		fromObj, _ := msg["from"].(map[string]interface{})
		ea, _ := fromObj["emailAddress"].(map[string]interface{})
		addr, _ := ea["address"].(string)
		return addr
	case "toRecipients":
		recipients, _ := msg["toRecipients"].([]interface{})
		if len(recipients) == 0 {
			return ""
		}
		rm, _ := recipients[0].(map[string]interface{})
		ea, _ := rm["emailAddress"].(map[string]interface{})
		addr, _ := ea["address"].(string)
		return addr
	}
	return ""
}
