package service

import (
	"net/http"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func TestGmailWriteOperationsRequests(t *testing.T) {
	cases := []struct {
		op       string
		path     string
		rawOf    func(body map[string]interface{}) (string, bool)
		wantItem string
	}{
		{"send_message", "/gmail/v1/users/me/messages/send", func(b map[string]interface{}) (string, bool) {
			raw, ok := b["raw"].(string)
			return raw, ok && len(b) == 1
		}, "msg_1"},
		{"create_draft", "/gmail/v1/users/me/drafts", func(b map[string]interface{}) (string, bool) {
			msg, _ := b["message"].(map[string]interface{})
			raw, ok := msg["raw"].(string)
			return raw, ok && len(b) == 1 && len(msg) == 1
		}, "msg_1"},
	}
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			c := newAPICapture(t, func(*http.Request) (int, string) { return http.StatusOK, `{"id":"msg_1"}` })
			c.point(t, &gmailBaseURL, "/gmail/v1/users/me")
			items := capRun(t, &GmailNode{}, workflow.NodeInput{}, map[string]interface{}{
				"access_token": "ya29", "operation": tc.op,
				"from": "me@x.co", "to": "a@x.co, b@x.co", "cc": "c@x.co", "bcc": "d@x.co",
				"subject": "Quarterly report", "body": "<p>Hi</p>", "body_type": "html",
			})
			if len(items) != 1 || items[0].JSON["id"] != tc.wantItem {
				t.Errorf("items = %+v", items)
			}
			req := c.only(t)
			if req.Method != "POST" || req.Path != tc.path {
				t.Errorf("request = %s %s, want POST %s", req.Method, req.Path, tc.path)
			}
			if got := req.Header.Get("Authorization"); got != "Bearer ya29" {
				t.Errorf("Authorization = %q", got)
			}
			if ct := req.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q", ct)
			}
			raw, ok := tc.rawOf(req.json(t))
			if !ok {
				t.Fatalf("unexpected body shape: %s", req.Body)
			}
			msg := gmailDecodeForTest(t, raw)
			for _, want := range []string{
				"From: me@x.co\r\n", "To: a@x.co, b@x.co\r\n", "Cc: c@x.co\r\n", "Bcc: d@x.co\r\n",
				"Subject: Quarterly report\r\n", "Content-Type: text/html; charset=\"UTF-8\"\r\n", "\r\n\r\n<p>Hi</p>",
			} {
				if !strings.Contains(msg, want) {
					t.Errorf("message missing %q:\n%s", want, msg)
				}
			}
		})
	}
}

func TestGmailSendMessageDefaultsToPlainText(t *testing.T) {
	c := newAPICapture(t, nil)
	c.point(t, &gmailBaseURL, "/gmail/v1/users/me")
	capRun(t, &GmailNode{}, workflow.NodeInput{}, map[string]interface{}{
		"access_token": "ya29", "operation": "send_message", "to": "a@x.co", "subject": "s", "body": "plain",
	})
	raw, _ := c.only(t).json(t)["raw"].(string)
	if msg := gmailDecodeForTest(t, raw); !strings.Contains(msg, "Content-Type: text/plain") {
		t.Errorf("message is not text/plain:\n%s", msg)
	}
}

func TestGmailSendMessageHTTPError(t *testing.T) {
	c := newAPICapture(t, func(*http.Request) (int, string) { return http.StatusUnauthorized, `{"error":"invalid_grant"}` })
	c.point(t, &gmailBaseURL, "/gmail/v1/users/me")
	err := capRunErr(t, &GmailNode{}, map[string]interface{}{"access_token": "ya29", "operation": "send_message", "to": "a@x.co"})
	capWantErr(t, err, "send_message", "401")
}
