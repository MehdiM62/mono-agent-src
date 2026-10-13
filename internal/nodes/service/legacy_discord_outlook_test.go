package service

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func withLegacyDiscord(t *testing.T) *legacyServer {
	t.Helper()
	srv := newLegacyServer(t, map[string]string{
		"POST /channels/C1/messages": `{"id":"M1","channel_id":"C1"}`,
		"GET /channels/C1":           `{"id":"C1","type":0,"guild_id":"G1"}`,
		"GET /guilds/G1/channels":    `[{"id":"C1","name":"general","type":0,"guild_id":"G1"}]`,
		"POST /api/webhooks/7/tok":   `{"id":"W1","channel_id":"C9"}`,
		"GET /api/webhooks/7/tok":    `{"id":"7","guild_id":"G9","channel_id":"C9"}`,
	})
	orig := discordBaseURL
	discordBaseURL = srv.URL
	t.Cleanup(func() { discordBaseURL = orig })
	return srv
}

// Saved comm.discord configs: "token" or "bot_token", optional "Bot "
// prefix, "content", no operation meaning send_message, and get_channels.
func TestLegacyDiscordConfigs(t *testing.T) {
	srv := withLegacyDiscord(t)

	cases := []struct {
		name   string
		config map[string]interface{}
		req    string
		check  func(t *testing.T, r recordedRequest, items []workflow.Item)
	}{
		{"send_message with token key and Bot prefix",
			map[string]interface{}{"token": "Bot abc", "operation": "send_message", "channel_id": "C1", "content": "hi"},
			"POST /channels/C1/messages",
			func(t *testing.T, r recordedRequest, items []workflow.Item) {
				if b := r.json(t); b["content"] != "hi" {
					t.Errorf("body = %v", b)
				}
				got := items[0].JSON
				if got["message_id"] != "M1" || got["channel_id"] != "C1" || got["guild_id"] != "G1" || got["channel_type"] != float64(0) {
					t.Errorf("output = %v", got)
				}
			}},
		{"no operation defaults to send_message",
			map[string]interface{}{"bot_token": "abc", "channel_id": "C1", "content": "hi"},
			"POST /channels/C1/messages",
			func(t *testing.T, r recordedRequest, items []workflow.Item) {
				if b := r.json(t); b["content"] != "hi" {
					t.Errorf("body = %v", b)
				}
			}},
		{"send_embed",
			map[string]interface{}{"bot_token": "abc", "operation": "send_embed", "channel_id": "C1", "embed_title": "T", "embed_description": "D", "embed_color": float64(255)},
			"POST /channels/C1/messages",
			func(t *testing.T, r recordedRequest, items []workflow.Item) {
				b := r.json(t)
				embeds, _ := b["embeds"].([]interface{})
				if len(embeds) != 1 {
					t.Fatalf("embeds = %v", b["embeds"])
				}
				e, _ := embeds[0].(map[string]interface{})
				if e["title"] != "T" || e["description"] != "D" || e["color"] != float64(255) {
					t.Errorf("embed = %v", e)
				}
				if _, ok := b["content"]; ok {
					t.Errorf("content set without text: %v", b)
				}
			}},
		{"get_channels",
			map[string]interface{}{"bot_token": "abc", "operation": "get_channels", "guild_id": "G1"},
			"GET /guilds/G1/channels",
			func(t *testing.T, r recordedRequest, items []workflow.Item) {
				got := items[0].JSON
				if len(items) != 1 || got["id"] != "C1" || got["name"] != "general" || got["guild_id"] != "G1" {
					t.Errorf("channels = %+v", items)
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv.reqs = nil
			cfg := map[string]interface{}{"credential_id": "discord"}
			for k, v := range tc.config {
				cfg[k] = v
			}
			items := runLegacy(t, &DiscordNode{}, cfg)
			r := srv.find(t, tc.req)
			if r.Auth != "Bot abc" {
				t.Errorf("Authorization = %q, want %q", r.Auth, "Bot abc")
			}
			tc.check(t, r, items)
		})
	}
}

func TestDiscordWebhookMode(t *testing.T) {
	srv := withLegacyDiscord(t)
	hook := srv.URL + "/api/webhooks/7/tok"

	t.Run("send_message", func(t *testing.T) {
		srv.reqs = nil
		items := runLegacy(t, &DiscordNode{}, map[string]interface{}{
			"auth": "webhook", "webhook_url": hook, "operation": "send_message", "text": "hi", "username": "Bot Name", "avatar_url": "https://a/x.png",
		})
		r := srv.find(t, "POST /api/webhooks/7/tok")
		if r.RawQuery != "wait=true" || r.Auth != "" {
			t.Errorf("query = %q, Authorization = %q (a webhook sends no auth header)", r.RawQuery, r.Auth)
		}
		if b := r.json(t); b["content"] != "hi" || b["username"] != "Bot Name" || b["avatar_url"] != "https://a/x.png" {
			t.Errorf("body = %v", b)
		}
		if got := items[0].JSON; got["message_id"] != "W1" || got["guild_id"] != "G9" {
			t.Errorf("output = %v (guild resolved from the webhook record)", got)
		}
	})

	t.Run("send_embed", func(t *testing.T) {
		srv.reqs = nil
		runLegacy(t, &DiscordNode{}, map[string]interface{}{
			"auth": "webhook", "webhook_url": hook, "operation": "send_embed", "embed_title": "T", "text": "above",
		})
		b := srv.find(t, "POST /api/webhooks/7/tok").json(t)
		if embeds, _ := b["embeds"].([]interface{}); len(embeds) != 1 || b["content"] != "above" {
			t.Errorf("body = %v", b)
		}
	})

	errCases := []struct {
		name, want string
		config     map[string]interface{}
	}{
		{"bot-only operation", "needs auth=bot", map[string]interface{}{"auth": "webhook", "webhook_url": hook, "operation": "list_messages"}},
		{"missing url", "'webhook_url' is required", map[string]interface{}{"auth": "webhook", "operation": "send_message", "text": "x"}},
		{"bad url", "must be an http(s) URL", map[string]interface{}{"auth": "webhook", "webhook_url": "discord.com/x", "text": "x"}},
		{"missing text", "'text' is required", map[string]interface{}{"auth": "webhook", "webhook_url": hook}},
		{"empty embed", "'embed_title' or 'embed_description'", map[string]interface{}{"auth": "webhook", "webhook_url": hook, "operation": "send_embed"}},
		{"unknown auth", "unknown auth", map[string]interface{}{"auth": "oauth", "bot_token": "x"}},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (&DiscordNode{}).Execute(context.Background(), workflow.NodeInput{}, tc.config)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// outlookTransportRecorder swaps the IMAP/SMTP transports for recorders.
func outlookTransportRecorder(t *testing.T) (read, send *[]map[string]interface{}) {
	t.Helper()
	var reads, sends []map[string]interface{}
	origRead, origSend := outlookIMAPRead, outlookSMTPSend
	outlookIMAPRead = func(_ context.Context, _ workflow.NodeInput, c map[string]interface{}) ([]workflow.NodeOutput, error) {
		reads = append(reads, c)
		return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{workflow.NewItem(map[string]interface{}{"subject": "s"})}}}, nil
	}
	outlookSMTPSend = func(_ context.Context, _ workflow.NodeInput, c map[string]interface{}) ([]workflow.NodeOutput, error) {
		sends = append(sends, c)
		return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{workflow.NewItem(map[string]interface{}{"sent": true})}}}, nil
	}
	t.Cleanup(func() { outlookIMAPRead, outlookSMTPSend = origRead, origSend })
	return &reads, &sends
}

// Saved comm.outlook_read / comm.outlook_send configs carry no operation and
// get email + app_password from an app-password outlook connection.
func TestLegacyOutlookConfigs(t *testing.T) {
	cases := []struct {
		name       string
		config     map[string]interface{}
		wantRead   bool
		checkInput func(t *testing.T, c map[string]interface{})
	}{
		{"comm.outlook_read", map[string]interface{}{
			"credential_id": "outlook", "email": "me@outlook.com", "app_password": "pw", "mailbox": "INBOX", "limit": float64(5), "unread_only": true,
		}, true, func(t *testing.T, c map[string]interface{}) {
			if c["mailbox"] != "INBOX" || c["limit"] != float64(5) || c["unread_only"] != true || c["email"] != "me@outlook.com" {
				t.Errorf("read config = %v", c)
			}
		}},
		{"comm.outlook_read with typed password", map[string]interface{}{"email": "me@outlook.com", "password": "pw"}, true, nil},
		{"comm.outlook_send", map[string]interface{}{
			"credential_id": "outlook", "email": "me@outlook.com", "app_password": "pw", "to": "a@x.com, b@x.com", "subject": "S", "body": "B", "body_type": "html",
		}, false, func(t *testing.T, c map[string]interface{}) {
			if c["to"] != "a@x.com, b@x.com" || c["subject"] != "S" || c["body"] != "B" || c["body_type"] != "html" {
				t.Errorf("send config = %v", c)
			}
		}},
		{"comm.outlook_send with an empty to expression", map[string]interface{}{"email": "me@outlook.com", "app_password": "pw", "to": "", "subject": "S", "body": "B"}, false, nil},
		{"canonical list_messages with max_results", map[string]interface{}{
			"auth": "app_password", "operation": "list_messages", "email": "me@outlook.com", "app_password": "pw", "max_results": float64(7),
		}, true, func(t *testing.T, c map[string]interface{}) {
			if c["limit"] != float64(7) {
				t.Errorf("limit = %v, want max_results carried over", c["limit"])
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reads, sends := outlookTransportRecorder(t)
			runLegacy(t, &OutlookMailNode{}, tc.config)
			if tc.wantRead && (len(*reads) != 1 || len(*sends) != 0) || !tc.wantRead && (len(*sends) != 1 || len(*reads) != 0) {
				t.Fatalf("reads=%d sends=%d, want read=%v", len(*reads), len(*sends), tc.wantRead)
			}
			if tc.checkInput != nil {
				if tc.wantRead {
					tc.checkInput(t, (*reads)[0])
				} else {
					tc.checkInput(t, (*sends)[0])
				}
			}
		})
	}
}

func TestOutlookAuthMode(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		want   string
		errSub string
	}{
		{"oauth connection", map[string]interface{}{"access_token": "t"}, outlookAuthOAuth, ""},
		{"app password connection", map[string]interface{}{"email": "e", "app_password": "p"}, outlookAuthAppPassword, ""},
		{"access token wins over password", map[string]interface{}{"access_token": "t", "app_password": "p"}, outlookAuthOAuth, ""},
		{"explicit app_password", map[string]interface{}{"auth": "app_password", "access_token": "t"}, outlookAuthAppPassword, ""},
		{"nothing set falls to oauth", map[string]interface{}{}, outlookAuthOAuth, ""},
		{"unknown", map[string]interface{}{"auth": "basic"}, "", "unknown auth"},
	}
	for _, tc := range cases {
		got, err := outlookAuthMode(tc.config)
		if tc.errSub != "" {
			if err == nil || !strings.Contains(err.Error(), tc.errSub) {
				t.Errorf("%s: err = %v, want %q", tc.name, err, tc.errSub)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}

func TestOutlookAppPasswordRejectsGraphOnlyOperations(t *testing.T) {
	outlookTransportRecorder(t)
	_, err := (&OutlookMailNode{}).Execute(context.Background(), workflow.NodeInput{}, map[string]interface{}{
		"auth": "app_password", "operation": "get_message", "email": "e", "app_password": "p", "message_id": "1",
	})
	if err == nil || !strings.Contains(err.Error(), "needs auth=oauth") {
		t.Errorf("err = %v", err)
	}
}
