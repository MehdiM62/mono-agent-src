package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/monoes/mono-agent/internal/credhttp/credhttptest"
)

// A service node's request carries the connection's token: a redirect to
// another host follows without it, and a 307 that would resend a body
// there isn't followed.
func TestServiceRequestsKeepTheTokenOnRedirect(t *testing.T) {
	get := credhttptest.NewPair(t, http.StatusFound, `{"ok":true}`)
	if _, err := apiRequest(context.Background(), http.MethodGet, get.From+"/products.json", "svc-tok", nil); err != nil {
		t.Fatal(err)
	}
	if got := get.Got(); len(got) != 1 || get.Leaked("svc-tok") != "" {
		t.Errorf("a GET's redirect carried %+v", got)
	}
	post := credhttptest.NewPair(t, http.StatusTemporaryRedirect, `{}`)
	_, _ = apiRequest(context.Background(), http.MethodPost, post.From+"/orders.json", "svc-tok", map[string]interface{}{"order": "1"})
	if got := post.Got(); len(got) != 0 {
		t.Errorf("a POST's 307 reached the other host: %+v", got)
	}
}
