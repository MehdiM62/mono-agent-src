package apipkg

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// An op with $map/$concat/fromjson/epoch_ms in its body and two "also"
// follow-ups, one reading res and one skipped by its when.
func TestAlsoStepsMapConcatAndFilters(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"update","title":"Update","method":"PUT","path":"/contacts/{{ p.contactId }}",
	  "fields":[{"key":"contactId","label":"Id","type":"text","required":true},
	            {"key":"emails","label":"Emails","type":"json"},{"key":"phones","label":"Phones","type":"json"},
	            {"key":"extraJson","label":"Extra","type":"text"},{"key":"closeDate","label":"Close","type":"text"},
	            {"key":"tags","label":"Tags","type":"json"},{"key":"star","label":"Star","type":"number"}],
	  "body":{"$merge":"{{ p.extraJson | fromjson }}","close":"{{ p.closeDate | epoch_ms }}",
	    "properties":{"$concat":[
	      {"$map":"{{ p.emails }}","$to":{"type":"SYSTEM","name":"email","subtype":"{{ el.subtype }}","value":"{{ el.email }}"}},
	      {"$map":"{{ p.phones }}","$to":{"type":"SYSTEM","name":"phone","value":"{{ el.number }}"}}]}},
	  "also":[{"when":"{{ p.tags }}","method":"PUT","path":"/contacts/{{ res.id }}/tags","body":{"tags":"{{ p.tags }}"}},
	          {"when":"{{ p.star }}","method":"PUT","path":"/contacts/{{ res.id }}/star"}]}`))
	var bodies []string
	seen := pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.Write([]byte(`{"id":"c9","ok":true}`))
	})
	items, err := runPackageOp(p, map[string]any{"operation": "update", "contactId": "c1",
		"emails":    []any{map[string]any{"subtype": "work", "email": "a@b.c"}},
		"phones":    []any{map[string]any{"number": "123"}, map[string]any{"number": ""}},
		"extraJson": `{"lead_score": 5}`, "closeDate": "2026-01-02T00:00:00Z",
		"tags": []any{"x"}}, cred, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 2 {
		t.Fatalf("want main + 1 also request (star skipped), got %d", len(*seen))
	}
	if r := (*seen)[1]; r.Method != "PUT" || r.URL.Path != "/v1/contacts/c9/tags" || r.Header.Get("Authorization") != "Bearer T" {
		t.Errorf("also request: %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
	}
	if bodies[1] != `{"tags":["x"]}` {
		t.Errorf("also body = %s", bodies[1])
	}
	for _, want := range []string{`"lead_score":5`, `"close":1767312000000`,
		`{"name":"email","subtype":"work","type":"SYSTEM","value":"a@b.c"}`,
		`{"name":"phone","type":"SYSTEM","value":"123"}`, `{"name":"phone","type":"SYSTEM"}`} {
		if !strings.Contains(bodies[0], want) {
			t.Errorf("main body %s lacks %s", bodies[0], want)
		}
	}
	if len(items) != 1 || items[0].JSON["id"] != "c9" {
		t.Errorf("output must be the main response, got %v", items)
	}
}

func TestMapConcatEdgeCasesAndNewFilters(t *testing.T) {
	sc := Scope{P: map[string]any{"none": nil, "list": []any{1.0, 2.0}, "js": `[1,2]`, "bad": `{`, "d": "2026-01-02"}}
	cases := []struct {
		in   any
		want any
	}{
		{map[string]any{"$map": "{{ p.none }}", "$to": "{{ el }}"}, nil},
		{map[string]any{"$map": "{{ p.list }}", "$to": map[string]any{"n": "{{ el }}"}}, []any{map[string]any{"n": 1.0}, map[string]any{"n": 2.0}}},
		{map[string]any{"$concat": []any{"{{ p.list }}", "{{ p.none }}", "x"}}, []any{1.0, 2.0, "x"}},
		{map[string]any{"$concat": []any{"{{ p.none }}"}}, nil},
		{"{{ p.js | fromjson }}", []any{1.0, 2.0}},
		{"{{ p.list | fromjson }}", []any{1.0, 2.0}},
		{"{{ p.d | epoch }}", int64(1767312000)},
		{"{{ p.d | epoch_ms }}", int64(1767312000000)},
		{"{{ p.none | epoch_ms }}", nil},
	}
	for _, c := range cases {
		got, err := renderValue(c.in, sc)
		if err != nil {
			t.Fatalf("%v: %v", c.in, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v = %#v, want %#v", c.in, got, c.want)
		}
	}
	for _, bad := range []any{"{{ p.bad | fromjson }}", "{{ p.js | epoch }}", map[string]any{"$map": "{{ p.list }}"},
		map[string]any{"$map": "{{ p.d }}", "$to": "x"}, map[string]any{"$concat": "x"}} {
		if _, err := renderValue(bad, sc); err == nil {
			t.Errorf("%v: want error", bad)
		}
	}
}

func TestAlsoValidation(t *testing.T) {
	_, err := Parse([]byte(pkgJSON(`{"resource":"","operation":"x","title":"X","method":"GET","path":"/x",
	  "also":[{"method":"GET","path":"/y/{{ p.nope }}"}]}`)), "test")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("also templates must be validated, got %v", err)
	}
}

func TestBatchOpSendsOneRequestForAllItems(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"batchCreate","title":"Batch","method":"POST","path":"/rows/batch","batch":true,
	  "fields":[{"key":"table","label":"Table","type":"text","required":true}],
	  "body":{"table":"{{ p.table }}","items":"{{ items }}"}}`))
	var bodies []string
	seen := pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.Write([]byte(`{"items":[{"id":1},{"id":2}]}`))
	})
	in := workflow.NodeInput{Items: []workflow.Item{{JSON: map[string]any{"name": "a"}}, {JSON: map[string]any{"name": "b"}}}}
	out, err := run(context.Background(), p, in, map[string]any{"operation": "batchCreate", "table": "t1", "credential": cred}, true)
	if err != nil {
		t.Fatal(err)
	}
	items := out[0].Items
	if len(*seen) != 1 || bodies[0] != `{"items":[{"name":"a"},{"name":"b"}],"table":"t1"}` {
		t.Fatalf("want one request with all items, got %d: %v", len(*seen), bodies)
	}
	if len(items) != 1 {
		t.Errorf("output items = %d", len(items))
	}
}

func TestKVPrefixSplitAndDate(t *testing.T) {
	sc := Scope{P: map[string]any{
		"rows": []any{map[string]any{"fieldId": 12.0, "fieldValue": "x"}},
		"csv":  "a, b,,c", "d": "2026-03-04T05:06:07Z", "ms": 1767312000000.0,
	}}
	cases := []struct {
		in   string
		want any
	}{
		{"{{ p.rows | kv:'fieldId,fieldValue,field_' }}", map[string]any{"field_12": "x"}},
		{"{{ p.csv | split:',' }}", []any{"a", "b", "c"}},
		{"{{ p.csv | split }}", []any{"a", "b", "c"}},
		{"{{ p.d | date:'YYYY-MM-DD' }}", "2026-03-04"},
		{"{{ p.d | date:'yyyy-MM-dd HH:mm:ss' }}", "2026-03-04 05:06:07"},
		{"{{ p.ms | date:'YYYY-MM-DD' }}", "2026-01-02"},
		{"{{ p.missing | date:'YYYY' }}", nil},
	}
	for _, c := range cases {
		got, err := renderString(c.in, sc)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s = %#v, want %#v", c.in, got, c.want)
		}
	}
}
