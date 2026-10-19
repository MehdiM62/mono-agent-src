package apipkg

import (
	"context"
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
