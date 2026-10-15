package connections

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// generic_oauth1 takes its endpoints and signature method from the
// connection's fields and keeps a non-default signature method.
func TestConnectOAuth1GenericPlatformUsesFieldEndpoints(t *testing.T) {
	mgr, _ := newManagerDB(t)
	prov := newFakeOAuth1Provider(t)
	port := freePort(t)
	stubOAuth1Browser(t, port, func(u *url.URL) string {
		return "oauth_token=" + u.Query().Get("oauth_token") + "&oauth_verifier=v123"
	})
	def := Registry["generic_oauth1"]
	orig := def.OAuth1
	def.OAuth1 = &OAuth1Config{CallbackPort: port}
	Registry["generic_oauth1"] = def
	t.Cleanup(func() { def.OAuth1 = orig; Registry["generic_oauth1"] = def })

	fields := map[string]string{
		OAuth1RequestTokenURL: prov.URL + "/request", OAuth1AuthorizationURL: prov.URL + "/authorize",
		OAuth1AccessTokenURL: prov.URL + "/access", OAuth1ConsumerKey: "ck", OAuth1ConsumerSecret: "cs",
		OAuth1SignatureMethod: "HMAC-SHA256", "base_url": "https://api.example.com",
	}
	conn, err := mgr.ConnectOAuth1(context.Background(), "generic_oauth1", fields, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{OAuth1Token: "final", OAuth1TokenSecret: "finalsecret",
		OAuth1SignatureMethod: "HMAC-SHA256", "base_url": "https://api.example.com", OAuth1RequestTokenURL: prov.URL + "/request"} {
		if getStr(conn.Data, k) != want {
			t.Errorf("data[%s] = %v, want %s", k, conn.Data[k], want)
		}
	}
	if h := prov.headers[0]; h["oauth_signature_method"] != "HMAC-SHA256" {
		t.Errorf("request-token header = %v", h)
	}

	delete(fields, OAuth1AccessTokenURL)
	if _, err := mgr.ConnectOAuth1(context.Background(), "generic_oauth1", fields, nil, ""); err == nil || !strings.Contains(err.Error(), OAuth1AccessTokenURL) {
		t.Errorf("missing access token URL: err = %v", err)
	}
}

func TestPromptMissingFieldsAsksOnlyMissingRequired(t *testing.T) {
	fields := []CredentialField{
		{Key: "a", Label: "A", Required: true},
		{Key: "b", Label: "B", Required: true},
		{Key: "c", Label: "C"},
	}
	values := map[string]string{"a": "given"}
	if err := PromptMissingFields(fields, values, strings.NewReader("typed\n")); err != nil {
		t.Fatal(err)
	}
	if values["a"] != "given" || values["b"] != "typed" || values["c"] != "" {
		t.Errorf("values = %v", values)
	}
	if err := PromptMissingFields(fields, map[string]string{"a": "x"}, strings.NewReader("\n")); err == nil || !strings.Contains(err.Error(), `"b"`) {
		t.Errorf("empty answer: err = %v", err)
	}
}
