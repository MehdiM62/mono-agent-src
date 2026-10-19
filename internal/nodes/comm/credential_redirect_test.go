package comm

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/monoes/mono-agent/internal/credhttp/credhttptest"
)

// Twilio's and WhatsApp's requests carry the account's token: a 307 that
// would resend the message there isn't followed, and a 302 (which drops
// the body) follows without the token.
func TestCommRequestsKeepTheTokenOnRedirect(t *testing.T) {
	post := credhttptest.NewPair(t, http.StatusTemporaryRedirect, `{}`)
	_, _ = twilioPost(context.Background(), post.From+"/Messages.json", "AC1", "tw-tok", url.Values{"Body": {"hi"}})
	_, _ = whatsappPost(context.Background(), post.From+"/messages", "wa-tok", map[string]interface{}{"to": "1"})
	if got := post.Got(); len(got) != 0 {
		t.Errorf("a POST's 307 reached the other host: %+v", got)
	}
	found := credhttptest.NewPair(t, http.StatusFound, `{}`)
	_, _ = twilioPost(context.Background(), found.From+"/Messages.json", "AC1", "tw-tok", url.Values{"Body": {"hi"}})
	_, _ = whatsappPost(context.Background(), found.From+"/messages", "wa-tok", map[string]interface{}{"to": "1"})
	if got := found.Got(); len(got) != 2 || got[0].Header.Get("Authorization") != "" || found.Leaked("wa-tok") != "" {
		t.Errorf("a 302 carried %+v", got)
	}
}
