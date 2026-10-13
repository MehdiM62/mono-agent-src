package apipkg

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestFindFindRowArithmeticColumnsGet(t *testing.T) {
	rows := []any{[]any{"id", "name"}, []any{"1", "ada"}, []any{"2", "bob"}}
	sc := Scope{
		P: map[string]any{"col": "name", "want": "bob", "start": 3.0, "n": "2", "letter": "C",
			"sheets": []any{map[string]any{"properties": map[string]any{"title": "S1", "sheetId": 7.0}}}},
		Item: map[string]any{"name": "ada"},
		Pre:  map[string]any{"values": rows, "headers": []any{map[string]any{"name": "Subject", "value": "hi"}}},
	}
	cases := []struct {
		in   string
		want any
	}{
		{"{{ pre.values | find_row:p.col,p.want }}", int64(3)},
		{"{{ pre.values | find_row:p.col,item }}", int64(2)},
		{"{{ pre.values | find_row:'name','zed' }}", nil},
		{"{{ pre.values | find_row:'nope','bob' }}", nil},
		{"{{ p.sheets | find:'properties.title','S1' | get:'properties.sheetId' }}", 7.0},
		{"{{ pre.headers | find:'name','Subject' | get:'value' }}", "hi"},
		{"{{ p.sheets | find:'properties.title','zz' }}", nil},
		{"{{ p.start | add:p.n }}", int64(5)},
		{"{{ p.start | sub:1 }}", int64(2)},
		{"{{ p.start | add:0.5 }}", 3.5},
		{"{{ p.letter | colnum }}", int64(3)},
		{"{{ p.start | colname }}", "C"},
	}
	for _, c := range cases {
		got, err := renderString(c.in, sc)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s = %#v, %v; want %#v", c.in, got, err, c.want)
		}
	}
	if v, _ := columnNumber("AA"); v != int64(27) {
		t.Errorf("AA = %v", v)
	}
	if v, _ := columnName(28); v != "AB" {
		t.Errorf("28 = %v", v)
	}
	for _, bad := range []string{"{{ p.col | find:'a' }}", "{{ p.col | find_row:'a','b','c' }}"} {
		if _, err := parseTemplate(bad); err == nil {
			t.Errorf("%s: want parse error", bad)
		}
	}
}

func TestEntriesObject(t *testing.T) {
	sc := Scope{P: map[string]any{"date": []any{
		map[string]any{"operation": "after", "value": "2024-01-01"},
		map[string]any{"operation": "before", "value": ""},
	}}}
	got, err := renderValue(map[string]any{"$entries": "{{ p.date }}", "$key": "date[{{ el.operation }}]", "$value": "{{ el.value | epoch }}"}, sc)
	if err != nil || !reflect.DeepEqual(got, map[string]any{"date[after]": int64(1704067200)}) {
		t.Errorf("entries = %#v, %v", got, err)
	}
	if _, err := renderValue(map[string]any{"$entries": "{{ p.date }}"}, sc); err == nil {
		t.Error("$entries without $key/$value must error")
	}
}

func TestOutputEachMatchExpand(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"list","title":"L","method":"GET","path":"/invoices",
	  "fields":[{"key":"filters","label":"F","type":"json"},{"key":"combine","label":"C","type":"text"},{"key":"first","label":"1","type":"boolean"}],
	  "output":{"path":"list","each":"invoice","match":{"rows":"{{ p.filters }}","column":"lookupColumn","value":"lookupValue","combine":"{{ p.combine }}","first":"{{ p.first }}"}}},
	  {"resource":"","operation":"msgs","title":"M","method":"GET","path":"/messages",
	  "output":{"path":"messages","expand":{"method":"GET","path":"/messages/{{ el.id }}","query":{"format":"metadata"}}}}`))
	seen := pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		switch {
		case r.URL.Path == "/v1/invoices":
			w.Write([]byte(`{"list":[{"invoice":{"id":"i1","status":"paid"}},{"invoice":{"id":"i2","status":"due"}},{"invoice":{"id":"i3","status":"paid"}}]}`))
		case r.URL.Path == "/v1/messages":
			w.Write([]byte(`{"messages":[{"id":"m1"},{"id":"m2"}]}`))
		default:
			id := strings.TrimPrefix(r.URL.Path, "/v1/messages/")
			w.Write([]byte(`{"id":"` + id + `","snippet":"s-` + id + `","format":"` + r.URL.Query().Get("format") + `"}`))
		}
	})
	paid := []any{map[string]any{"lookupColumn": "status", "lookupValue": "paid"}}
	items, err := runPackageOp(p, map[string]any{"operation": "list", "filters": paid}, cred, nil)
	if err != nil || len(items) != 2 || items[1].JSON["id"] != "i3" {
		t.Fatalf("each+match: %v %v", items, err)
	}
	items, _ = runPackageOp(p, map[string]any{"operation": "list", "filters": paid, "first": true}, cred, nil)
	if len(items) != 1 {
		t.Errorf("first match: %d items", len(items))
	}
	or := []any{map[string]any{"lookupColumn": "id", "lookupValue": "i1"}, map[string]any{"lookupColumn": "id", "lookupValue": "i2"}}
	items, _ = runPackageOp(p, map[string]any{"operation": "list", "filters": or, "combine": "OR"}, cred, nil)
	if len(items) != 2 {
		t.Errorf("OR: %d items", len(items))
	}
	items, _ = runPackageOp(p, map[string]any{"operation": "list"}, cred, nil)
	if len(items) != 3 {
		t.Errorf("no filters keeps all: %d", len(items))
	}
	*seen = nil
	items, err = runPackageOp(p, map[string]any{"operation": "msgs"}, cred, nil)
	if err != nil || len(items) != 2 || items[1].JSON["snippet"] != "s-m2" || items[0].JSON["format"] != "metadata" || len(*seen) != 3 {
		t.Errorf("expand: %v %v (%d requests)", items, err, len(*seen))
	}
}

// Sheets append with a gid locator: resolve the title, then read its
// header, then append the item ordered by the header.
func TestBeforeStepsList(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"append","title":"A","method":"POST",
	  "path":"/s/values/{{ pres.0.sheets | find:'properties.sheetId',p.gid | get:'properties.title' }}:append",
	  "fields":[{"key":"gid","label":"G","type":"number","required":true}],
	  "before":[{"method":"GET","path":"/s","query":{"fields":"sheets.properties"}},
	            {"method":"GET","path":"/s/values/{{ pre.sheets | find:'properties.sheetId',p.gid | get:'properties.title' }}!1:1"}],
	  "body":{"values":["{{ item | row:pre.values }}"]}}`))
	var paths []string
	pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		paths = append(paths, r.URL.Path)
		switch n {
		case 1:
			w.Write([]byte(`{"sheets":[{"properties":{"sheetId":0,"title":"Main"}},{"properties":{"sheetId":42,"title":"Leads"}}]}`))
		case 2:
			w.Write([]byte(`{"values":[["email","name"]]}`))
		default:
			w.Write([]byte(`{}`))
		}
	})
	if _, err := runPackageOp(p, map[string]any{"operation": "append", "gid": 42.0}, cred, map[string]any{"name": "ada", "email": "a@b"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"/v1/s", "/v1/s/values/Leads!1:1", "/v1/s/values/Leads:append"}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

func TestSheetGIDParenArgsAndRowNull(t *testing.T) {
	sheets := []any{map[string]any{"properties": map[string]any{"sheetId": 0.0, "title": "Main"}},
		map[string]any{"properties": map[string]any{"sheetId": 123.0, "title": "Leads"}}}
	sc := Scope{P: map[string]any{"sheets": sheets,
		"list": map[string]any{"__rl": true, "mode": "list", "value": "gid=0"},
		"url":  map[string]any{"__rl": true, "mode": "url", "value": "https://docs.google.com/spreadsheets/d/abc/edit#gid=123"},
		"id":   map[string]any{"__rl": true, "mode": "id", "value": "123"},
		"num":  123.0},
		Item: map[string]any{"a": 1.0}, Pre: map[string]any{"values": []any{[]any{"a", "b"}}}}
	cases := map[string]any{
		"{{ p.list | sheetgid }}": "0",
		"{{ p.url | sheetgid }}":  "123",
		"{{ p.id | sheetgid }}":   "123",
		"{{ p.num | sheetgid }}":  "123",
		"{{ p.sheets | find:'properties.sheetId',(p.url | sheetgid) | get:'properties.title' }}":  "Leads",
		"{{ p.sheets | find:'properties.sheetId',(p.list | sheetgid) | get:'properties.title' }}": "Main",
		"{{ p.sheets | get:(p.num | sheetgid) }}":                                                 nil,
	}
	for in, want := range cases {
		if got, err := renderString(in, sc); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %#v, %v; want %#v", in, got, err, want)
		}
	}
	for in, want := range map[string]any{
		"{{ item | row:pre.values }}":        []any{1.0, ""},
		"{{ item | row:pre.values,'null' }}": []any{1.0, nil},
		"{{ item | row:pre.values,null }}":   []any{1.0, nil},
	} {
		if got, err := renderString(in, sc); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %#v, %v; want %#v", in, got, err, want)
		}
	}
}

// Contentful: the token and output shape depend on node params.
func TestInjectReadsParamsAndTemplatedOutput(t *testing.T) {
	p := mustParse(t, `{"$schema":"apipkg.v1","id":"cf","title":"CF","base_url":"https://cdn.test",
	  "credentials":[{"platform":"cf","methods":["bearer"],"fields":[{"key":"access_token","label":"T","required":true},{"key":"preview_token","label":"P"}],
	    "inject":{"headers":{"Authorization":"Bearer {{ p.source | eq:'previewApi' | then:cred.preview_token | default:cred.access_token }}"}}}],
	  "operations":[{"resource":"","operation":"get","title":"Get","method":"GET","path":"/entries",
	    "fields":[{"key":"source","label":"S","type":"text"},{"key":"rawData","label":"R","type":"boolean"}],
	    "output":{"path":"items","each":"{{ p.rawData | ne:'true' | then:'fields' }}"}}]}`)
	var auths []string
	pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		auths = append(auths, r.Header.Get("Authorization"))
		w.Write([]byte(`{"items":[{"sys":{"id":"e1"},"fields":{"title":"Hi"}}]}`))
	})
	c := map[string]any{"access_token": "D", "preview_token": "P"}
	items, err := runPackageOp(p, map[string]any{"operation": "get", "source": "previewApi"}, c, nil)
	if err != nil || len(items) != 1 || items[0].JSON["title"] != "Hi" {
		t.Fatalf("simple: %v %v", items, err)
	}
	items, _ = runPackageOp(p, map[string]any{"operation": "get", "rawData": true}, c, nil)
	if len(items) != 1 || items[0].JSON["fields"] == nil {
		t.Errorf("rawData: %v", items)
	}
	if !reflect.DeepEqual(auths, []string{"Bearer P", "Bearer D"}) {
		t.Errorf("auth headers = %v", auths)
	}
}

func TestEntriesFilterAndOutputTo(t *testing.T) {
	sc := Scope{P: map[string]any{"keyName": "row"}, Item: map[string]any{"row": map[string]any{"B": 2.0, "A": "x"}}}
	got, err := renderString("{{ item | get:p.keyName | entries:'column,value' }}", sc)
	want := []any{map[string]any{"column": "A", "value": "x"}, map[string]any{"column": "B", "value": 2.0}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("entries = %#v, %v", got, err)
	}
	if got, _ := renderString("{{ item.row | entries }}", sc); len(got.([]any)) != 2 || got.([]any)[0].(map[string]any)["key"] != "A" {
		t.Errorf("default entries = %#v", got)
	}
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"rows","title":"R","method":"GET","path":"/rows",
	  "fields":[{"key":"rawData","label":"R","type":"boolean"}],
	  "output":{"path":"items","to":{"id":"{{ el.id }}","$merge":"{{ el.values }}"},"to_when":"{{ p.rawData | ne:'true' }}"}}`))
	pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		w.Write([]byte(`{"items":[{"id":"r1","values":{"Name":"ada"}}]}`))
	})
	items, err := runPackageOp(p, map[string]any{"operation": "rows"}, cred, nil)
	if err != nil || items[0].JSON["Name"] != "ada" || items[0].JSON["id"] != "r1" {
		t.Errorf("to: %v %v", items, err)
	}
	items, _ = runPackageOp(p, map[string]any{"operation": "rows", "rawData": true}, cred, nil)
	if items[0].JSON["values"] == nil {
		t.Errorf("to_when false keeps raw: %v", items)
	}
}

// The harness itself: responses answer in order and expect_requests /
// the default main-request check see past before steps.
func TestHarnessMultiRequestCases(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"append","title":"A","method":"POST","path":"/s/v/Main:append",
	  "before":[{"method":"GET","path":"/s/v/Main!1:1"}],
	  "body":{"values":["{{ item | row:pre.values }}"]}}`))
	op := &p.Operations[0]
	var c opCase
	if err := json.Unmarshal([]byte(`{"input":{"name":"ada"},
	  "responses":[{"values":[["name","email"]]},{"updates":{}}],
	  "expect":{"method":"POST","path":"/v1/s/v/Main:append","body":{"values":[["ada",""]]}}}`), &c); err != nil {
		t.Fatal(err)
	}
	checkCase(t, p, op, c)
	c.ExpectRequests = []expectReq{{Method: "GET", Path: "/v1/s/v/Main!1:1"}, c.Expect}
	checkCase(t, p, op, c)
}

func TestOutputToOnSingleObject(t *testing.T) {
	p := mustParse(t, pkgJSON(`{"resource":"","operation":"get","title":"G","method":"GET","path":"/row",
	  "output":{"to":{"id":"{{ el.id }}","$merge":"{{ el.values }}"}}}`))
	pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		w.Write([]byte(`{"id":"r1","values":{"Name":"ada"}}`))
	})
	items, err := runPackageOp(p, map[string]any{"operation": "get"}, cred, nil)
	if err != nil || len(items) != 1 || items[0].JSON["Name"] != "ada" || items[0].JSON["values"] != nil {
		t.Errorf("single object to: %v %v", items, err)
	}
}
