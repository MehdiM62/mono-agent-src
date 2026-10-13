package apipkg

import (
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
