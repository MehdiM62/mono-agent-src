package httpnodes

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"hash"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
)

// rfc3986 percent-encodes all but the unreserved characters.
func rfc3986(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// verifyOAuth1 checks r's OAuth 1.0a signature the way a provider does,
// from the consumer and token secrets, and returns the header's params.
func verifyOAuth1(t *testing.T, r *http.Request, body, consumerSecret, tokenSecret string) map[string]string {
	t.Helper()
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "OAuth ") {
		t.Fatalf("Authorization %q is not OAuth", h)
	}
	oauth := map[string]string{}
	for _, part := range strings.Split(strings.TrimPrefix(h, "OAuth "), ", ") {
		k, v, _ := strings.Cut(part, "=")
		v, _ = url.PathUnescape(strings.Trim(v, `"`))
		oauth[k] = v
	}
	var pairs []string
	add := func(vals url.Values) {
		for k, vs := range vals {
			for _, v := range vs {
				pairs = append(pairs, rfc3986(k)+"="+rfc3986(v))
			}
		}
	}
	add(r.URL.Query())
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		form, _ := url.ParseQuery(body)
		add(form)
	}
	for k, v := range oauth {
		if k != "oauth_signature" && k != "realm" {
			pairs = append(pairs, rfc3986(k)+"="+rfc3986(v))
		}
	}
	sort.Strings(pairs)
	base := r.Method + "&" + rfc3986("http://"+r.Host+r.URL.Path) + "&" + rfc3986(strings.Join(pairs, "&"))
	var hf func() hash.Hash = sha1.New
	if oauth["oauth_signature_method"] == "HMAC-SHA256" {
		hf = sha256.New
	}
	m := hmac.New(hf, []byte(rfc3986(consumerSecret)+"&"+rfc3986(tokenSecret)))
	m.Write([]byte(base))
	if want := base64.StdEncoding.EncodeToString(m.Sum(nil)); oauth["oauth_signature"] != want {
		t.Fatalf("oauth_signature = %q, want %q (base %s)", oauth["oauth_signature"], want, base)
	}
	return oauth
}

func TestRequestSignsWithOAuth1Connection(t *testing.T) {
	data := map[string]interface{}{
		"consumer_key": "ck", "consumer_secret": "c s&1",
		"oauth_token": "tok", "oauth_token_secret": "ts~2",
	}
	cases := []struct {
		name   string
		data   map[string]interface{}
		extra  map[string]interface{}
		method string
	}{
		{"get with query", data, map[string]interface{}{"query_params": map[string]interface{}{"q": "a b", "n": "1"}}, "HMAC-SHA1"},
		{"form body", data, map[string]interface{}{"method": "POST", "body_type": "form", "body": map[string]interface{}{"status": "Hello Ladies + Gentlemen"}}, "HMAC-SHA1"},
		{"sha256 from the connection", withField(data, "signature_method", "HMAC-SHA256"), nil, "HMAC-SHA256"},
		{"n8n oAuth1Api alias", data, map[string]interface{}{"auth_type": "oAuth1Api"}, "HMAC-SHA1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecorder(t, nil)
			mustOK(t, &RequestNode{}, credCfg(rec.URL+"/1.1/statuses?x=y", tc.data, tc.extra))
			got := rec.last(t)
			r, _ := http.NewRequest(got.Method, rec.URL+got.Path, nil)
			r.URL.RawQuery = url.Values(got.Query).Encode()
			r.Header = got.Header
			r.Host = got.Host
			oauth := verifyOAuth1(t, r, got.Body, "c s&1", "ts~2")
			if oauth["oauth_consumer_key"] != "ck" || oauth["oauth_token"] != "tok" || oauth["oauth_signature_method"] != tc.method {
				t.Errorf("header params = %v", oauth)
			}
		})
	}
}

func withField(m map[string]interface{}, k string, v interface{}) map[string]interface{} {
	out := map[string]interface{}{k: v}
	for kk, vv := range m {
		out[kk] = vv
	}
	return out
}

func TestRequestOAuth1Errors(t *testing.T) {
	cases := []struct {
		data  map[string]interface{}
		extra map[string]interface{}
		want  string
	}{
		{map[string]interface{}{"consumer_key": "ck", "oauth_token": "t", "signature_method": "RSA-SHA1"}, nil, "RSA-SHA1"},
		{map[string]interface{}{"api_key": "PRIVATE-VALUE"}, map[string]interface{}{"credential_auth": "oauth1"}, "no consumer_key"},
	}
	for i, tc := range cases {
		rec := newRecorder(t, nil)
		_, e := execute(t, &RequestNode{}, credCfg(rec.URL, tc.data, tc.extra))
		if len(e) != 1 || !strings.Contains(e[0].JSON["error"].(string), tc.want) || strings.Contains(e[0].JSON["error"].(string), "PRIVATE-VALUE") {
			t.Fatalf("case %d: errors = %v", i, e)
		}
		if len(rec.all()) != 0 {
			t.Fatalf("case %d: the request was sent", i)
		}
	}
}
