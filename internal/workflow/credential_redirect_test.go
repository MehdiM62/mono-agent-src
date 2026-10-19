package workflow

import (
	"context"
	"net/http"
	"testing"

	"github.com/monoes/mono-agent/internal/credhttp/credhttptest"
)

// A trigger.sse stream's headers and an app webhook's registration carry
// a token: a redirect to another host follows without it, and a 307 that
// would resend a registration body there isn't followed.
func TestTriggerAndWebhookRequestsKeepTheTokenOnRedirect(t *testing.T) {
	sse := credhttptest.NewPair(t, http.StatusFound, `{}`)
	c := &sseConn{src: &SSETriggerSource{}, url: sse.From + "/events", headers: map[string]string{"X-Api-Key": "sse-k3y"}}
	_, _ = c.stream(context.Background(), func(Item) {})
	if got := sse.Got(); len(got) != 1 || sse.Leaked("sse-k3y") != "" {
		t.Errorf("the stream's redirect carried %+v", got)
	}

	hook := AppWebhook{URL: "https://hooks.example/x", Config: map[string]interface{}{"credential": map[string]interface{}{"access_token": "hook-t0k"}}}
	post := credhttptest.NewPair(t, http.StatusTemporaryRedirect, `{}`)
	h := &HTTPRegistrar{BaseURL: post.From, Auth: "Bearer {{credential.access_token}}"}
	_, _, _ = h.do(context.Background(), HTTPCall{Method: http.MethodPost, URL: "/subscriptions", Body: map[string]interface{}{"url": "{{url}}"}}, hook, AppWebhookHook{})
	if got := post.Got(); len(got) != 0 {
		t.Errorf("a registration's 307 reached the other host: %+v", got)
	}
	get := credhttptest.NewPair(t, http.StatusFound, `{}`)
	h.BaseURL = get.From
	_, _, _ = h.do(context.Background(), HTTPCall{Method: http.MethodGet, URL: "/subscriptions/1"}, hook, AppWebhookHook{})
	if got := get.Got(); len(got) != 1 || get.Leaked("hook-t0k") != "" {
		t.Errorf("a check's redirect carried %+v", got)
	}
}
