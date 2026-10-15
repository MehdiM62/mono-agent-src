package apipkg

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// The n8n Google Sheets append shape: read the header row first (before),
// then send the item as a values row ordered by that header.
func TestBeforeStepRowFilterAndHeaderRowOutput(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"append","title":"Append","method":"POST",
	  "path":"/sheets/{{ p.sheet }}/values/{{ p.sheet | prefix:'S!' }}:append",
	  "fields":[{"key":"sheet","label":"Sheet","type":"text","required":true}],
	  "before":{"method":"GET","path":"/sheets/{{ p.sheet }}/values/1:1"},
	  "body":{"values":["{{ item | row:pre.values }}"]}},
	  {"resource":"","operation":"read","title":"Read","method":"GET","path":"/read","output":{"path":"values","header_row":true}}`))
	var bodies []string
	seen := pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		switch {
		case strings.HasSuffix(r.URL.Path, "/values/1:1"):
			w.Write([]byte(`{"values":[["name","email","age"]]}`))
		case r.URL.Path == "/v1/read":
			w.Write([]byte(`{"values":[["name","age"],["ada","36"],["bob"]]}`))
		default:
			w.Write([]byte(`{"updates":{"updatedRows":1}}`))
		}
	})
	if _, err := runPackageOp(p, map[string]any{"operation": "append", "sheet": "abc"}, cred,
		map[string]any{"email": "a@b.c", "name": "ada", "extra": "x"}); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 2 || (*seen)[1].URL.Path != "/v1/sheets/abc/values/S!abc:append" {
		t.Fatalf("requests: %d, last %v", len(*seen), (*seen)[len(*seen)-1].URL.Path)
	}
	if bodies[1] != `{"values":[["ada","a@b.c",""]]}` {
		t.Errorf("append body = %s", bodies[1])
	}
	items, err := runPackageOp(p, map[string]any{"operation": "read"}, cred, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].JSON["name"] != "ada" || items[0].JSON["age"] != "36" || items[1].JSON["name"] != "bob" {
		t.Errorf("header_row items = %v", items)
	}
}

func TestPrefixSuffixBase64UUID(t *testing.T) {
	sc := Scope{P: map[string]any{"r": "FREQ=DAILY", "none": "", "s": "hi?"}}
	cases := []struct {
		in   string
		want any
	}{
		{"{{ p.r | prefix:'RRULE:' }}", "RRULE:FREQ=DAILY"},
		{"{{ p.none | prefix:'RRULE:' }}", nil},
		{"{{ p.r | suffix:'!' }}", "FREQ=DAILY!"},
		{"{{ p.s | base64 }}", "aGk/"},
		{"{{ p.s | base64url }}", "aGk_"},
	}
	for _, c := range cases {
		got, err := renderString(c.in, sc)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s = %#v, %v; want %#v", c.in, got, err, c.want)
		}
	}
	a, _ := renderString("{{ uuid }}", sc)
	b, _ := renderString("{{ uuid }}", sc)
	if s, ok := a.(string); !ok || len(s) != 36 || a == b {
		t.Errorf("uuid = %v, %v", a, b)
	}
}

func TestScopeGroupCheck(t *testing.T) {
	p := mustParse(t, `{"$schema":"apipkg.v1","id":"gcal","title":"Cal","base_url":"https://x.test",
	  "credentials":[{"platform":"gcal","methods":["bearer"],"scope_group":"calendar","fields":[{"key":"access_token","label":"T","required":true}],
	    "inject":{"headers":{"Authorization":"Bearer {{ cred.access_token }}"}}}],
	  "operations":[{"resource":"","operation":"get","title":"Get","method":"GET","path":"/t"}]}`)
	old := ScopeCheck
	t.Cleanup(func() { ScopeCheck = old })
	var gotGroup string
	ScopeCheck = func(_ map[string]any, g string) error {
		gotGroup = g
		return errors.New("missing scope: connect google --scopes calendar")
	}
	_, err := runPackageOp(p, map[string]any{"operation": "get"}, map[string]any{"access_token": "t"}, nil)
	if err == nil || gotGroup != "calendar" || !strings.Contains(err.Error(), "--scopes calendar") {
		t.Errorf("scope check: group %q, err %v", gotGroup, err)
	}
}

func TestWhenGatesObjects(t *testing.T) {
	sc := Scope{P: map[string]any{"loc": "endOfSegmentLocation", "seg": ""}}
	in := map[string]any{"insertText": map[string]any{
		"text":                 "hi",
		"endOfSegmentLocation": map[string]any{"$when": "{{ p.loc | eq:'endOfSegmentLocation' }}", "segmentId": "{{ p.seg }}"},
		"location":             map[string]any{"$when": "{{ p.loc | eq:'location' }}", "index": 1.0},
	}}
	got, err := renderValue(in, sc)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"insertText": map[string]any{"text": "hi", "endOfSegmentLocation": map[string]any{}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestGoogleFileID(t *testing.T) {
	id := "1AbCdEfGhIjKlMnOpQrStUvWxYz0123456789"
	cases := map[string]any{
		"https://docs.google.com/document/d/" + id + "/edit":                id,
		"https://docs.google.com/spreadsheets/d/" + id + "/edit#gid=0":      id,
		"https://drive.google.com/drive/folders/" + id:                      id,
		"https://drive.google.com/open?id=" + id:                            id,
		"https://docs.google.com/presentation/d/" + id + "/edit#slide=id.p": id,
		id:                            id,
		"https://example.com/d/" + id: "https://example.com/d/" + id,
	}
	for in, want := range cases {
		if got := googleFileID(in); got != want {
			t.Errorf("%s → %v, want %v", in, got, want)
		}
	}
	sc := Scope{P: map[string]any{"doc": map[string]any{"__rl": true, "mode": "url", "value": "https://docs.google.com/document/d/" + id + "/edit"},
		"byid": map[string]any{"__rl": true, "mode": "id", "value": "abc"}}}
	for in, want := range map[string]any{"{{ p.doc | rl }}": id, "{{ p.byid | rl }}": "abc", "{{ p.doc.value | gid }}": id} {
		if got, err := renderString(in, sc); err != nil || got != want {
			t.Errorf("%s = %v, %v", in, got, err)
		}
	}
}
