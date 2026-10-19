package hilsend

import "testing"

func TestPlatformOfIsTheNodesOwnName(t *testing.T) {
	for node, want := range map[string]string{
		"service.discord": "discord", "comm.discord": "discord", "discord": "discord",
		"service.reddit": "reddit", "comm.reddit": "reddit",
		"comm.telegram": "telegram", "telegram": "telegram", "comm.twilio": "twilio", "comm.whatsapp": "whatsapp", "comm.slack": "slack",
		"service.mastodon": "mastodon", "comm.mastodon": "mastodon", "service.bluesky": "bluesky",
		"service.devto": "devto", "service.hashnode": "hashnode", "service.producthunt": "producthunt", "service.youtube": "youtube",
		"comm.email_send": "email", "email_send": "email", "comm.outlook_send": "outlook", "service.outlook_mail": "outlook", "service.gmail": "gmail",
		"instagram.send_dms": "instagram", "linkedin.send_dms": "linkedin",
	} {
		if got := PlatformOf(node); got != want {
			t.Errorf("PlatformOf(%q) = %q, want %q", node, got, want)
		}
	}
}

// A rule's platform is the node's own, not its family: a rule for Discord
// channel 123 never stands for a Reddit post to a thing or subreddit that
// reads "123", nor the other way round (second security review of #871).
// comm.discord and service.discord are one platform.
func TestARuleNeverCrossesPlatformsOfOneFamily(t *testing.T) {
	discord := map[string]interface{}{"operation": "send_message", "bot_token": "x", "channel_id": "123", "text": "hi"}
	st := allowFor(t, "service.discord", discord)
	for _, reddit := range []map[string]interface{}{
		{"operation": "submit_post", "access_token": "x", "subreddit": "123", "title": "t", "text": "hi"},
		{"operation": "comment", "access_token": "x", "thing_id": "123", "text": "hi"},
	} {
		if h, _ := held(t, st, "service.reddit", reddit); !h {
			t.Errorf("a Reddit %s to \"123\" went out on the Discord rule", reddit["operation"])
		}
	}
	if h, p := held(t, st, "comm.discord", discord); h {
		t.Errorf("the legacy name of the same Discord send was held: %+v", p.Account)
	}
	rules, _ := st.Allows(t.Context())
	if len(rules) != 1 || rules[0].Platform != "discord" {
		t.Fatalf("rules = %+v", rules)
	}
}
