package apipkg

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// A package's credential, and every header rendered from the connection,
// goes only to a host the package names: its base URL's host or a
// subdomain of it, a host written in the op's path, or one of its
// auth_hosts. An absolute URL a param or a response names elsewhere is
// sent without them.
func TestCredentialGoesOnlyToHostsThePackageNames(t *testing.T) {
	pkg := &Package{ID: "t", BaseURL: "https://api.example.test/v1", AuthHosts: []string{"*.regional.example", "media.example"},
		Headers: map[string]string{"X-Account": "{{ cred.token }}"}}
	cred := &Credential{Platform: "t", Inject: Inject{
		Headers: map[string]string{"Authorization": "Bearer {{ cred.token }}"}, Query: map[string]string{"key": "{{ cred.token }}"}}}
	cases := []struct {
		path, param, next string
		authed            bool
	}{
		{path: "/items", authed: true},
		{path: "https://api.example.test/upload", authed: true},
		{path: "https://upload.example.test/files", authed: true},
		{path: "{{ p.url }}", param: "https://eu.api.example.test/x", authed: true},
		{path: "{{ p.url }}", param: "https://db1.regional.example/x.json", authed: true},
		{path: "{{ p.url }}", param: "https://media.example/f", authed: true},
		{path: "{{ p.url }}", param: "https://attacker.test/collect"},
		{path: "{{ p.url }}", param: "https://api.example.test.attacker.test/x"},
		{path: "{{ p.url }}", param: "https://regional.example.attacker.test/x"},
		{path: "{{ p.url }}", param: "https://upload.example.test/files"},
		{path: "https://{{ p.url }}/x", param: "attacker.test"},
		{path: "/items", next: "https://attacker.test/page2"},
		{path: "/items", next: "https://api.example.test/v1/items?page=2", authed: true},
		// A next page a response names gets the credential only on a host
		// the package names, however it dresses another one up.
		{path: "/items", next: "https://API.Example.Test./v1/items?page=3", authed: true},
		{path: "/items", next: "https://api.example.test@attacker.test/p"},
		{path: "/items", next: "https://attacker.test/https://api.example.test/v1"},
		{path: "/items", next: "https://api.example.test.attacker.test/p"},
		{path: "/items", next: "//attacker.test/p"},
		{path: "/items", next: "https://attacker.test/?next=https://api.example.test"},
	}
	for _, c := range cases {
		r := &opRun{pkg: pkg, op: &Operation{Method: "GET", Path: c.path}, credDef: cred,
			scope: Scope{P: map[string]any{"url": c.param}, Cred: map[string]any{"token": "sekret-1"}}}
		b, err := r.buildRequest(context.Background(), pageParams{url: c.next})
		if err != nil {
			t.Fatalf("%s %s %s: %v", c.path, c.param, c.next, err)
		}
		h := b.req.Header
		got := h.Get("Authorization") == "Bearer sekret-1" && b.req.URL.Query().Get("key") == "sekret-1" && h.Get("X-Account") == "sekret-1"
		none := h.Get("Authorization") == "" && b.req.URL.Query().Get("key") == "" && h.Get("X-Account") == ""
		if (c.authed && !got) || (!c.authed && !none) {
			t.Errorf("%s (p.url %q, next %q) to %s: headers %v, query %q; want the credential sent %v", c.path, c.param, c.next, b.req.URL.Host, h, b.req.URL.RawQuery, c.authed)
		}
		if r.scope.Cred["token"] != "sekret-1" {
			t.Fatalf("the run's credential scope was left blank: %v", r.scope.Cred)
		}
	}

	// A base URL whose host a param renders (an S3 region) names a host
	// only as the package and the connection render it, or in auth_hosts;
	// the connection's own URL fields name theirs.
	regional := &Package{ID: "r", BaseURL: "https://s3.{{ p.region | default:cred.region }}.storage.example", AuthHosts: []string{"*.storage.example"}}
	for region, authed := range map[string]bool{"eu-west-1": true, "": true, "attacker.test/x": false, "attacker.test#": false} {
		r := &opRun{pkg: regional, op: &Operation{Method: "GET", Path: "/bucket"}, credDef: cred,
			scope: Scope{P: map[string]any{"region": region}, Cred: map[string]any{"token": "sekret-1", "region": "us-east-1"}}}
		b, err := r.buildRequest(context.Background(), pageParams{})
		if err != nil {
			t.Fatalf("region %q: %v", region, err)
		}
		if got := b.req.Header.Get("Authorization") != ""; got != authed {
			t.Errorf("region %q to %s: credential sent %v, want %v", region, b.req.URL.Host, got, authed)
		}
	}
	// A self-hosted variant a param picks (Jira Server's PAT): its host is
	// the connection's own.
	self := &Package{ID: "s", BaseURL: "{{ p.server | eq:'self' | then:cred.domain | default:'https://api.vendor.example' }}"}
	for path, authed := range map[string]bool{"": true, "https://files.my-site.example/a": true, "https://attacker.test/a": false} {
		op := &Operation{Method: "GET", Path: "{{ p.url }}"}
		if path == "" {
			op.Path = "/items"
		}
		r := &opRun{pkg: self, op: op, credDef: cred,
			scope: Scope{P: map[string]any{"url": path, "server": "self"}, Cred: map[string]any{"token": "sekret-1", "domain": "https://my-site.example"}}}
		b, err := r.buildRequest(context.Background(), pageParams{})
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if got := b.req.Header.Get("Authorization") != ""; got != authed {
			t.Errorf("%s: credential sent %v, want %v", path, got, authed)
		}
	}
}

// aws_s3 bucket/create renders its host from the region param: a region
// that names a host of its own gets neither the signature nor the session
// token; a real region still signs.
func TestS3RegionCantMoveTheCredential(t *testing.T) {
	p := catalogPkg(t, "aws_s3")
	op, ok := p.Op("bucket", "create")
	if !ok {
		t.Fatal("aws_s3 has no bucket/create")
	}
	cred := map[string]any{"access_key_id": "AKIDEXAMPLE", "secret_access_key": "secret", "session_token": "sess-tok-1", "region": "us-east-1"}
	for _, c := range []struct {
		region string
		signed bool
	}{
		{"eu-west-1", true}, {"", true},
		{"evil.test#", false}, {"evil.test/", false}, {"evil.test?x=", false}, {"evil.test:8443#", false},
	} {
		r := &opRun{pkg: p, op: op, credDef: p.selectCredential(cred),
			scope: Scope{P: map[string]any{"name": "b1", "additionalFields": map[string]any{"region": c.region}}, Cred: cred}}
		b, err := r.buildRequest(context.Background(), pageParams{})
		if err != nil {
			t.Fatalf("region %q: %v", c.region, err)
		}
		h := b.req.Header
		signed := strings.HasPrefix(h.Get("Authorization"), "AWS4-HMAC-SHA256 ") && h.Get("X-Amz-Security-Token") == "sess-tok-1"
		bare := h.Get("Authorization") == "" && h.Get("X-Amz-Security-Token") == ""
		if (c.signed && !signed) || (!c.signed && !bare) {
			t.Errorf("region %q to %s: Authorization %q, X-Amz-Security-Token %q; want signed %v", c.region, b.req.URL.Host, h.Get("Authorization"), h.Get("X-Amz-Security-Token"), c.signed)
		}
	}
}

// A redirect to another host carries none of the headers the credential
// set, the X-Api-Key Go keeps included; one on the same host keeps them.
func TestRedirectToAnotherHostDropsTheCredential(t *testing.T) {
	type seen struct{ path, auth, key, account string }
	var mu sync.Mutex
	var got []seen
	record := func(r *http.Request) {
		mu.Lock()
		got = append(got, seen{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Api-Key"), r.Header.Get("X-Account")})
		mu.Unlock()
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer other.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		switch r.URL.Path {
		case "/away":
			http.Redirect(w, r, other.URL+"/landing", http.StatusFound)
		case "/here":
			http.Redirect(w, r, "/landed", http.StatusFound)
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer api.Close()
	pkg := &Package{ID: "t", BaseURL: api.URL, Headers: map[string]string{"X-Account": "{{ cred.account }}"}}
	cred := &Credential{Platform: "t", Inject: Inject{Headers: map[string]string{"Authorization": "Bearer {{ cred.token }}", "X-Api-Key": "{{ cred.token }}"}}}
	for _, path := range []string{"/away", "/here"} {
		got = nil
		r := &opRun{pkg: pkg, op: &Operation{Method: "GET", Path: path}, credDef: cred,
			scope: Scope{Cred: map[string]any{"token": "sekret-1", "account": "acct-7"}}}
		b, err := r.buildRequest(context.Background(), pageParams{})
		if err != nil {
			t.Fatal(err)
		}
		resp, err := r.client().Do(b.req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if len(got) != 2 || got[0].key != "sekret-1" {
			t.Fatalf("%s: requests %+v", path, got)
		}
		want := seen{"/landing", "", "", ""}
		if path == "/here" {
			want = seen{"/landed", "Bearer sekret-1", "sekret-1", "acct-7"}
		}
		if got[1] != want {
			t.Errorf("%s: the redirected request carried %+v, want %+v", path, got[1], want)
		}
	}
}

// paramHosts are the API package base URLs whose host part reads a param
// or a response, and why the credential still goes only to a host the
// package names (authHost renders the base from the package and the
// connection alone). A new one fails until it is looked at.
var paramHosts = map[string]string{
	"aws_s3":     "the region a param or GetBucketLocation picks; signed only for *.amazonaws.com (auth_hosts)",
	"contentful": "p.source picks preview or cdn.contentful.com; *.contentful.com (auth_hosts)",
	"jira":       "api.atlassian.com, written in the package, or the connection's own domain",
	"storyblok":  "p.source picks mapi or api.storyblok.com, both written in the package",
}

func TestAPackagesBaseHostFromAParamIsReviewed(t *testing.T) {
	pkgs, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	ref := regexp.MustCompile(`\b(p|pre|pres|item|el|res|hdr|page|main)\.`)
	seen := map[string]bool{}
	for _, p := range pkgs {
		bases := []string{p.BaseURL}
		for i := range p.Operations {
			bases = append(bases, p.Operations[i].BaseURL)
		}
		for _, b := range bases {
			if !ref.MatchString(hostPart(b)) {
				continue
			}
			seen[p.ID] = true
			if paramHosts[p.ID] == "" {
				t.Errorf("%s: base_url %q takes its host from a param or a response: review it and list it in paramHosts with why the credential can't follow it", p.ID, b)
			}
		}
	}
	for id := range paramHosts {
		if !seen[id] {
			t.Errorf("paramHosts lists %s, whose base hosts no longer read a param", id)
		}
	}
}

// hostPart is a URL template up to the first "/" after its scheme that
// isn't inside {{ }}.
func hostPart(tmpl string) string {
	i := strings.Index(tmpl, "://")
	if i < 0 {
		i = 0
	} else {
		i += 3
	}
	depth := 0
	for j := i; j < len(tmpl); j++ {
		switch {
		case strings.HasPrefix(tmpl[j:], "{{"):
			depth++
			j++
		case strings.HasPrefix(tmpl[j:], "}}"):
			depth--
			j++
		case tmpl[j] == '/' && depth == 0:
			return tmpl[:j]
		}
	}
	return tmpl
}

// A 307 or 308 to another host would send a request the credential went
// on again, body and all, and the body may hold the credential
// (inject.body): such a redirect isn't followed, for JSON, form and
// multipart bodies alike. A request without the credential follows it.
func TestRedirectNeverReplaysTheCredentialsBody(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer other.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := http.StatusTemporaryRedirect
		if r.URL.Path == "/permanent" {
			code = http.StatusPermanentRedirect
		}
		http.Redirect(w, r, other.URL+"/landing", code)
	}))
	defer api.Close()
	pkg := &Package{ID: "t", BaseURL: api.URL}
	cred := &Credential{Platform: "t", Inject: Inject{Body: map[string]string{"api_key": "{{ cred.token }}"}}}
	for _, c := range []struct {
		path, bodyType string
		credential     bool
	}{
		{"/away", BodyJSON, true}, {"/permanent", BodyJSON, true}, {"/away", BodyForm, true}, {"/away", BodyMultipart, true},
		{"/away", BodyJSON, false},
	} {
		bodies = nil
		r := &opRun{pkg: pkg, op: &Operation{Method: "POST", Path: c.path, BodyType: c.bodyType, Body: map[string]any{"x": "1"}}}
		if c.credential {
			r.credDef, r.scope = cred, Scope{Cred: map[string]any{"token": "SECRET-T"}}
		}
		b, err := r.buildRequest(context.Background(), pageParams{})
		if err != nil {
			t.Fatal(err)
		}
		resp, err := r.client().Do(b.req)
		if err != nil {
			t.Fatalf("%s %s: %v", c.path, c.bodyType, err)
		}
		resp.Body.Close()
		mu.Lock()
		got := append([]string(nil), bodies...)
		mu.Unlock()
		if c.credential {
			if len(got) != 0 || resp.StatusCode < 300 {
				t.Errorf("%s %s with the credential: the other host got %q, status %d", c.path, c.bodyType, got, resp.StatusCode)
			}
			continue
		}
		if len(got) != 1 || strings.Contains(got[0], "SECRET") {
			t.Errorf("%s %s without the credential: the other host got %q, want the body once", c.path, c.bodyType, got)
		}
	}
}

// From https to plain http on the same host is leaving it too: the
// credential's headers go no further. https to https on the same host
// keeps them.
func TestRedirectToPlainHTTPDropsTheCredential(t *testing.T) {
	first := withCredentialHeaders(httptest.NewRequest("GET", "https://api.example.test/x", nil), []string{"Authorization", "X-Api-Key"})
	for target, kept := range map[string]bool{
		"https://api.example.test/y":      true,
		"http://api.example.test/y":       false,
		"https://api.example.test:8443/y": false,
		"https://other.example.test/y":    false,
	} {
		req := httptest.NewRequest("GET", target, nil)
		req.Header.Set("Authorization", "Bearer t")
		req.Header.Set("X-Api-Key", "k")
		if err := dropCredentialsOnRedirect(req, []*http.Request{first}); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if got := req.Header.Get("Authorization") != "" && req.Header.Get("X-Api-Key") != ""; got != kept {
			t.Errorf("%s: headers %v, want the credential kept %v", target, req.Header, kept)
		}
	}
}
