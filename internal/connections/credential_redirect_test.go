package connections

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/credhttp"
	"github.com/monoes/mono-agent/internal/credhttp/credhttptest"
)

// A token exchange carries the client secret, a code or a refresh token:
// a 307 or 308 to another host would send it there again, so it isn't
// followed (form, Reddit's Basic auth, Notion's JSON alike), and OAuth
// 1.0a's bodiless POST follows without its signature.
func TestTokenRequestsKeepTheirSecretOnRedirect(t *testing.T) {
	for _, code := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		p := credhttptest.NewPair(t, code, `{}`)
		form := url.Values{"client_id": {"cid"}, "client_secret": {"s3cret-c"}, "refresh_token": {"r3fresh"}, "grant_type": {"refresh_token"}}
		for _, platform := range []string{"github", "reddit", "notion"} {
			// The error names where the redirect went, not "token
			// endpoint returned status 307".
			if _, _, err := postForm(platform, p.From+"/token", form); !errors.Is(err, credhttp.ErrResend) ||
				!strings.Contains(err.Error(), "redirected to "+strings.TrimPrefix(p.To, "http://")) {
				t.Errorf("%d %s: %v; want the redirect refused, naming the target", code, platform, err)
			}
		}
		if got := p.Got(); len(got) != 0 {
			t.Errorf("%d: the other host got %+v", code, got)
		}
		// OAuth 1.0a's POST has no body: it follows, without its signed
		// Authorization header.
		_, _ = oauth1Post(p.From+"/oauth/request_token", OAuth1Params{ConsumerKey: "ck", ConsumerSecret: "cs-secret", Token: "tk", TokenSecret: "ts-secret"})
		if got := p.Got(); len(got) != 1 || got[0].Header.Get("Authorization") != "" {
			t.Errorf("%d: OAuth 1.0a's redirect carried %+v", code, got)
		}
	}
}

// A validation request carries the connection's token: a redirect to
// another host follows without it.
func TestValidationRequestsKeepTheTokenOnRedirect(t *testing.T) {
	p := credhttptest.NewPair(t, http.StatusFound, `{"login":"me"}`)
	if _, status, err := doGET(context.Background(), p.From+"/user", "Bearer tok-v4l1d"); err != nil || status != http.StatusOK {
		t.Fatalf("doGET: %d %v", status, err)
	}
	if got := p.Got(); len(got) != 1 || got[0].Header.Get("Authorization") != "" {
		t.Errorf("the other host got %+v, want the GET without the token", got)
	}
}
