package resources

import (
	"context"
	"net/http"
	"testing"

	"github.com/monoes/mono-agent/internal/credhttp/credhttptest"
)

// A provider request carries the connection's token: a redirect to
// another host follows without it, and a 307 that would resend a body
// there isn't followed; a Client with no HTTP of its own too.
func TestResourceRequestsKeepTheTokenOnRedirect(t *testing.T) {
	for _, c := range []*Client{New(), {}} {
		get := credhttptest.NewPair(t, http.StatusFound, `{}`)
		if _, _, err := c.do(context.Background(), http.MethodGet, get.From+"/files", "res-tok", nil); err != nil {
			t.Fatal(err)
		}
		if got := get.Got(); len(got) != 1 || get.Leaked("res-tok") != "" {
			t.Errorf("a GET's redirect carried %+v", got)
		}
		post := credhttptest.NewPair(t, http.StatusTemporaryRedirect, `{}`)
		_, _, _ = c.do(context.Background(), http.MethodPost, post.From+"/files", "res-tok", map[string]interface{}{"name": "x"})
		if got := post.Got(); len(got) != 0 {
			t.Errorf("a POST's 307 reached the other host: %+v", got)
		}
	}
}
