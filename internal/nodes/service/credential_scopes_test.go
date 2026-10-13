package service

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// Each Google/Microsoft node accepts an old per-product connection and an
// umbrella connection granted its scopes, and refuses an umbrella connection
// lacking them with the exact connect command to run — before any request.
func TestUmbrellaCredentialScopesPerNode(t *testing.T) {
	googleAll := "openid https://www.googleapis.com/auth/spreadsheets https://www.googleapis.com/auth/drive " +
		"https://www.googleapis.com/auth/gmail.modify https://www.googleapis.com/auth/youtube.upload https://www.googleapis.com/auth/youtube.force-ssl"
	cases := []struct {
		node     workflow.NodeExecutor
		prefix   string
		umbrella string
		granted  string // umbrella grant covering the node
		lacking  string // umbrella grant missing the node's scopes
		command  string
	}{
		{&GoogleSheetsNode{}, "google_sheets", "google", googleAll, "https://www.googleapis.com/auth/gmail.modify", "monoagentcli connect google --scopes sheets,gmail"},
		{&GmailNode{}, "gmail", "google", googleAll, "https://www.googleapis.com/auth/drive", "monoagentcli connect google --scopes gmail,drive"},
		{&GoogleDriveNode{}, "google_drive", "google", googleAll, "https://www.googleapis.com/auth/calendar.events", "monoagentcli connect google --scopes drive,calendar"},
		{&YouTubeNode{}, "youtube", "google", googleAll, "https://www.googleapis.com/auth/gmail.modify", "monoagentcli connect google --scopes gmail,youtube"},
		{&OutlookMailNode{}, "outlook_mail", "microsoft", "Mail.ReadWrite Mail.Send User.Read offline_access", "https://graph.microsoft.com/Chat.ReadWrite", "monoagentcli connect microsoft --scopes mail"},
	}
	for _, tc := range cases {
		t.Run(tc.prefix, func(t *testing.T) {
			run := func(cred map[string]interface{}) error {
				cfg := map[string]interface{}{"access_token": "tok", "operation": "no_such_operation", "spreadsheet_id": "sid", "credential": cred}
				_, err := tc.node.Execute(context.Background(), workflow.NodeInput{}, cfg)
				return err
			}
			legacy := map[string]interface{}{"access_token": "tok", "scope": "anything"}
			if err := run(legacy); err == nil || strings.Contains(err.Error(), "scope group") {
				t.Errorf("legacy connection: err = %v, want the unknown-operation error", err)
			}
			ok := map[string]interface{}{"access_token": "tok", "oauth_platform": tc.umbrella, "scope": tc.granted}
			if err := run(ok); err == nil || strings.Contains(err.Error(), "scope group") {
				t.Errorf("granted umbrella: err = %v, want the unknown-operation error", err)
			}
			lacking := map[string]interface{}{"access_token": "tok", "oauth_platform": tc.umbrella, "scope": tc.lacking}
			err := run(lacking)
			if err == nil || !strings.HasPrefix(err.Error(), tc.prefix+": ") || !strings.HasSuffix(err.Error(), "add it with: "+tc.command) {
				t.Errorf("lacking umbrella: err = %v, want re-consent hint %q", err, tc.command)
			}
		})
	}
}
