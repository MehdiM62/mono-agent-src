package apipkg

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateReportsEveryProblem(t *testing.T) {
	cases := map[string]struct {
		js   string
		want []string
	}{
		"schema": {`{"$schema":"apipkg.v1","id":"Bad","title":"","base_url":"x","quey":1,"operations":[]}`, []string{
			`$.id: "Bad" does not match`, `$.title: must not be empty`, `unknown key "quey"`, `$.operations: needs at least 1 item`,
		}},
		"op schema": {pkgJSON(`{"resource":"","operation":"x","title":"X","method":"FETCH","path":"/x","fields":[{"key":"a","label":"A","type":"widget"}],"pagination":{"type":"cursr"}}`), []string{
			`.method: "FETCH" is not one of`, `.type: "widget" is not one of`, `.pagination.type: "cursr" is not one of`,
		}},
		"semantics": {pkgJSON(`{"resource":"task","operation":"get","title":"G","method":"GET","path":"tasks/{{ p.taskId }}",
		  "query":{"x":"{{ p.nope }}","y":"{{ cred.nokey }}","z":"{{ p.a | wat }}"},
		  "fields":[{"key":"a","label":"A","type":"text"},{"key":"a","label":"A2","type":"text"},{"key":"resource","label":"R","type":"text"},{"key":"s","label":"S","type":"select"}],
		  "pagination":{"type":"cursor","cursor_param":"c"},"binary":{"upload_field":"file"},"n8n_param_map":{"old":"gone"}},
		  {"resource":"task","operation":"get","title":"G2","method":"GET","path":"/t"}`), []string{
			`operation "task.get" is defined twice`,
			`resource "task" set but the package has no resources[]`,
			`path "tasks/{{ p.taskId }}" must start with /`,
			`{{ p.taskId }}: "taskId" is not one of the op's fields`,
			`{{ p.nope }}: "nope" is not one of the op's fields`,
			`{{ cred.nokey }}: "nokey" is not a credential field`,
			`unknown filter "wat"`,
			`field "a" is defined twice`,
			`field key "resource" is reserved`,
			`field "s": a select needs options`,
			`pagination "cursor" needs cursor_param and cursor_path`,
			`binary: "file" is not one of the op's fields`,
			`n8n_param_map: "old" maps to "gone"`,
		}},
		"credentials": {`{"$schema":"apipkg.v1","id":"c","title":"C","base_url":"https://{{ cred.sub }}.x.test","node_type":"app.c","extends":"service.github",
		  "credentials":[{"platform":"github","methods":["oauth","api_key"],"inject":{}},{"platform":"c2","methods":["api_key"],"reuse_platform":"nope",
		    "fields":[{"key":"refresh_token","label":"R"}],"inject":{"headers":{"X":"{{ p.a }}"}}}],
		  "resources":[{"value":"r","label":"R"},{"value":"r","label":"R"},{"value":"unused","label":"U"}],
		  "operations":[{"resource":"r","operation":"o","title":"O","method":"GET","path":"/o"}]}`, []string{
			`set node_type or extends, not both`,
			`resources: "r" is listed twice`,
			`resources: "unused" has no operations`,
			`platform "github" is a built-in connection platform: set reuse_platform`,
			`method "oauth" needs an "oauth" block`,
			`method "api_key" needs fields[]`,
			`inject is empty`,
			`reuse_platform "nope" is not a built-in connection platform`,
			`field "refresh_token" clashes with an OAuth token key`,
			`{{ cred.sub }}: "sub" is not a credential field`,
		}},
	}
	for name, c := range cases {
		_, err := Parse([]byte(c.js), name+".json")
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: want a ValidationError, got %v", name, err)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, name+".json is invalid") {
			t.Errorf("%s: the error must name the file: %s", name, msg)
		}
		for _, w := range c.want {
			if !strings.Contains(msg, w) {
				t.Errorf("%s: missing %q in:\n%s", name, w, msg)
			}
		}
	}
	if _, err := Parse([]byte(`{nope`), "x.json"); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("bad JSON: %v", err)
	}
}

func TestValidMinimalPackage(t *testing.T) {
	p := mustParse(t, `{"$schema":"apipkg.v1","id":"min","title":"Min","base_url":"https://x.test",
	  "operations":[{"resource":"","operation":"ping","title":"Ping","method":"GET","path":"/ping"}]}`)
	if p.NodeTypeOf() != "app.min" || p.needsCredential() || p.PrimaryPlatform() != "" {
		t.Errorf("%+v", p)
	}
}
