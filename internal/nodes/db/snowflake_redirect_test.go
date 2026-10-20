package dbnodes

import (
	"context"
	"net/http"
	"testing"

	"github.com/monoes/mono-agent/internal/credhttp/credhttptest"
)

// A Snowflake SQL API request carries the token: a redirect to another
// host follows without it, and a 307 that would resend the statement
// there isn't followed.
func TestSnowflakeRequestsKeepTheTokenOnRedirect(t *testing.T) {
	get := credhttptest.NewPair(t, http.StatusFound, `{"statementHandle":""}`)
	c, err := newSnowflakeClient(map[string]interface{}{"account": "acct", "base_url": get.From, "token": "sf-tok"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.do(context.Background(), http.MethodGet, get.From+"/api/v2/statements/h1", nil)
	if got := get.Got(); len(got) != 1 || get.Leaked("sf-tok") != "" {
		t.Errorf("a GET's redirect carried %+v", got)
	}
	post := credhttptest.NewPair(t, http.StatusTemporaryRedirect, `{}`)
	_, _ = c.do(context.Background(), http.MethodPost, post.From+"/api/v2/statements", map[string]interface{}{"statement": "SELECT 1"})
	if got := post.Got(); len(got) != 0 {
		t.Errorf("a POST's 307 reached the other host: %+v", got)
	}
}
