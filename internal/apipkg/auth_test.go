package apipkg

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// AWS's published SigV4 test-suite vector "get-vanilla".
func TestSigV4GetVanilla(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://example.amazonaws.com/", nil)
	err := signSigV4(req, nil, sigV4Params{
		Service: "service", Region: "us-east-1",
		AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
	}, time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, " +
		"SignedHeaders=host;x-amz-date, Signature=5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization:\n got %s\nwant %s", got, want)
	}
	if req.Header.Get("X-Amz-Date") != "20150830T123600Z" {
		t.Errorf("X-Amz-Date: %s", req.Header.Get("X-Amz-Date"))
	}
}

func TestSigV4ThroughInject(t *testing.T) {
	p := mustParse(t, `{"$schema":"apipkg.v1","id":"awsdemo","title":"AWS demo","base_url":"https://s3.{{ cred.region }}.amazonaws.com",
	  "credentials":[{"platform":"awsdemo","methods":["api_key"],
	    "fields":[{"key":"access_key_id","label":"Key","required":true},{"key":"secret_access_key","label":"Secret","secret":true,"required":true},
	              {"key":"region","label":"Region","required":true},{"key":"session_token","label":"Session","secret":true}],
	    "inject":{"aws_sigv4":{"service":"s3","region":"{{ cred.region }}"}}}],
	  "operations":[{"resource":"","operation":"list","title":"List","method":"GET","path":"/{{ p.bucket }}",
	    "fields":[{"key":"bucket","label":"Bucket","type":"text","required":true}]}]}`)
	old := sigV4Now
	sigV4Now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	t.Cleanup(func() { sigV4Now = old })
	srv := newMockServer(t, map[string]any{}, 200)
	_, err := runPackageOp(p, map[string]any{"bucket": "my-bucket"},
		map[string]any{"access_key_id": "AK", "secret_access_key": "SK", "region": "eu-west-1", "session_token": "ST"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.requests()[0].header
	auth := h.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AK/20260102/eu-west-1/s3/aws4_request, SignedHeaders=") ||
		!strings.Contains(auth, "x-amz-content-sha256;x-amz-date;x-amz-security-token") {
		t.Errorf("Authorization: %s", auth)
	}
	if h.Get("X-Amz-Security-Token") != "ST" || h.Get("X-Amz-Content-Sha256") == "" {
		t.Errorf("headers: %v", h)
	}
	_, err = runPackageOp(p, map[string]any{"bucket": "b"}, map[string]any{"access_key_id": "AK", "region": "r"}, nil)
	if err == nil || !strings.Contains(err.Error(), "secret access key") {
		t.Errorf("missing secret: %v", err)
	}
}

func TestInjectQueryBasicAndSelection(t *testing.T) {
	p := mustParse(t, `{"$schema":"apipkg.v1","id":"multi","title":"Multi","base_url":"https://x.test",
	  "credentials":[
	    {"platform":"multi_key","methods":["api_key"],"fields":[{"key":"key","label":"Key","required":true},{"key":"token","label":"Token","required":true}],
	     "inject":{"query":{"key":"{{ cred.key }}","token":"{{ cred.token }}"}}},
	    {"platform":"multi_basic","methods":["basic"],"fields":[{"key":"email","label":"Email","required":true},{"key":"api_token","label":"Token","secret":true,"required":true}],
	     "inject":{"basic":{"username":"{{ cred.email }}","password":"{{ cred.api_token }}"}}},
	    {"platform":"multi_oauth","methods":["oauth"],"oauth":{"auth_url":"https://x.test/a","token_url":"https://x.test/t"},
	     "inject":{"headers":{"Authorization":"Bearer {{ cred.access_token }}"}}}],
	  "operations":[{"resource":"","operation":"get","title":"Get","method":"GET","path":"/me","query":{"q":"1"}}]}`)
	cases := []struct {
		name  string
		cred  map[string]any
		check func(c captured) bool
	}{
		{"query", map[string]any{"key": "K", "token": "T"}, func(c captured) bool {
			return c.query.Get("key") == "K" && c.query.Get("token") == "T" && c.query.Get("q") == "1" && c.header.Get("Authorization") == ""
		}},
		{"basic", map[string]any{"email": "a@b.c", "api_token": "pw"}, func(c captured) bool {
			return c.header.Get("Authorization") == "Basic YUBiLmM6cHc=" && !c.query.Has("key")
		}},
		{"oauth", map[string]any{"access_token": "AT", "refresh_token": "RT"}, func(c captured) bool {
			return c.header.Get("Authorization") == "Bearer AT"
		}},
	}
	for _, c := range cases {
		srv := newMockServer(t, map[string]any{}, 200)
		if _, err := runPackageOp(p, nil, c.cred, nil); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := srv.requests()[0]; !c.check(got) {
			t.Errorf("%s: query %v header %v", c.name, got.query, got.header)
		}
	}
}

func TestEmptyBearerHeaderNotSent(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"get","title":"Get","method":"GET","path":"/x"}`))
	srv := newMockServer(t, map[string]any{}, 200)
	if _, err := runPackageOp(p, nil, map[string]any{"other": "v"}, nil); err != nil {
		t.Fatal(err)
	}
	if h := srv.requests()[0].header.Get("Authorization"); h != "" {
		t.Errorf("a header whose expressions are all empty must not be sent, got %q", h)
	}
}

func TestNoneCredentialRunsWithout(t *testing.T) {
	p := mustParse(t, `{"$schema":"apipkg.v1","id":"open","title":"Open","base_url":"https://x.test",
	  "credentials":[{"platform":"open_api","methods":["none","api_key"],"fields":[{"key":"key","label":"Key"}],"inject":{"headers":{"X-Key":"{{ cred.key }}"}}}],
	  "operations":[{"resource":"","operation":"get","title":"Get","method":"GET","path":"/x"}]}`)
	newMockServer(t, map[string]any{}, 200)
	if _, err := runPackageOp(p, nil, nil, nil); err != nil {
		t.Errorf("a credential with method none is optional: %v", err)
	}
}
