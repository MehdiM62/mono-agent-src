package data

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func items(t *testing.T, raw string) []workflow.Item {
	t.Helper()
	var objs []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &objs); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	out := make([]workflow.Item, len(objs))
	for i, o := range objs {
		out[i] = workflow.Item{JSON: o}
	}
	return out
}

func run(t *testing.T, node workflow.NodeExecutor, in []workflow.Item, config map[string]interface{}) []map[string]interface{} {
	t.Helper()
	out, err := node.Execute(context.Background(), workflow.NodeInput{Items: in}, config)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var res []map[string]interface{}
	for _, it := range out[0].Items {
		res = append(res, it.JSON)
	}
	return res
}

func asJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func wantJSON(t *testing.T, got interface{}, want string) {
	t.Helper()
	var w interface{}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("bad want: %v", err)
	}
	var g interface{}
	_ = json.Unmarshal([]byte(asJSON(got)), &g)
	if !reflect.DeepEqual(g, w) {
		t.Errorf("got  %s\nwant %s", asJSON(got), want)
	}
}

func TestSplitOut(t *testing.T) {
	in := `[{"id":1,"tags":["a","b"],"users":[{"n":"x"},{"n":"y"}],"meta":{"k":"v"}}]`
	cases := []struct {
		name   string
		config map[string]interface{}
		want   string
	}{
		{"scalars keep field name", map[string]interface{}{"field_to_split_out": "tags"}, `[{"tags":"a"},{"tags":"b"}]`},
		{"objects are merged", map[string]interface{}{"field_to_split_out": "$json.users"}, `[{"n":"x"},{"n":"y"}]`},
		{"destination", map[string]interface{}{"field_to_split_out": "users", "destination_field_name": "user"}, `[{"user":{"n":"x"}},{"user":{"n":"y"}}]`},
		{"destination path", map[string]interface{}{"field_to_split_out": "tags", "destination_field_name": "out.tag"}, `[{"out":{"tag":"a"}},{"out":{"tag":"b"}}]`},
		{"all other fields", map[string]interface{}{"field_to_split_out": "tags", "include": "allOtherFields"}, `[{"id":1,"users":[{"n":"x"},{"n":"y"}],"meta":{"k":"v"},"tags":"a"},{"id":1,"users":[{"n":"x"},{"n":"y"}],"meta":{"k":"v"},"tags":"b"}]`},
		{"selected fields", map[string]interface{}{"field_to_split_out": "tags", "include": "selected_other_fields", "fields_to_include": "id, meta.k"}, `[{"tags":"a","id":1,"meta.k":"v"},{"tags":"b","id":1,"meta.k":"v"}]`},
		{"multiple fields zip", map[string]interface{}{"field_to_split_out": "tags,users"}, `[{"tags":"a","users":{"n":"x"}},{"tags":"b","users":{"n":"y"}}]`},
		{"object values", map[string]interface{}{"field_to_split_out": "meta"}, `[{"meta":"v"}]`},
		{"missing field", map[string]interface{}{"field_to_split_out": "nope"}, `null`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantJSON(t, run(t, &SplitOutNode{}, items(t, in), c.config), c.want)
		})
	}
}

func TestSplitOutErrorsAndBinary(t *testing.T) {
	node := &SplitOutNode{}
	in := workflow.NodeInput{Items: items(t, `[{"a":[1]}]`)}
	for _, cfg := range []map[string]interface{}{
		{},
		{"field_to_split_out": "a,b", "destination_field_name": "x"},
		{"field_to_split_out": "a", "include": "selected_other_fields"},
		{"field_to_split_out": "a", "include": "bogus"},
	} {
		if _, err := node.Execute(context.Background(), in, cfg); err == nil {
			t.Errorf("config %v: expected error", cfg)
		}
	}
	bin := workflow.Item{JSON: map[string]interface{}{"list": []interface{}{1.0, 2.0}}, Binary: map[string][]byte{"a": []byte("A"), "b": []byte("B")}}
	out, err := node.Execute(context.Background(), workflow.NodeInput{Items: []workflow.Item{bin}}, map[string]interface{}{"field_to_split_out": "$binary"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out[0].Items) != 2 || string(out[0].Items[0].Binary["a"]) != "A" || string(out[0].Items[1].Binary["b"]) != "B" {
		t.Errorf("binary split: %+v", out[0].Items)
	}
	out, _ = node.Execute(context.Background(), workflow.NodeInput{Items: []workflow.Item{bin}}, map[string]interface{}{"field_to_split_out": "list", "include_binary": true})
	if len(out[0].Items) != 2 || string(out[0].Items[1].Binary["b"]) != "B" {
		t.Errorf("include_binary: %+v", out[0].Items)
	}
}

const salesFixture = `[
 {"country":"DE","city":"Berlin","amount":10,"tag":"x","obj":{"a":1}},
 {"country":"DE","city":"Hamburg","amount":5,"tag":"","obj":{"a":1}},
 {"country":"FR","city":"Paris","amount":7.5,"tag":"y"},
 {"country":"FR","city":"Paris","amount":"3","tag":"y"}
]`

func TestSummarizeAggregations(t *testing.T) {
	cfg := map[string]interface{}{"fields_to_summarize": `[
		{"aggregation":"count","field":"tag"},
		{"aggregation":"count","field":"city","include_empty":true},
		{"aggregation":"countUnique","field":"tag"},
		{"aggregation":"sum","field":"amount"},
		{"aggregation":"average","field":"amount"},
		{"aggregation":"min","field":"amount"},
		{"aggregation":"max","field":"city"},
		{"aggregation":"append","field":"tag"},
		{"aggregation":"concatenate","field":"city","separate_by":"other","custom_separator":"|"},
		{"aggregation":"concatenate","field":"obj"}
	]`}
	got := run(t, &SummarizeNode{}, items(t, salesFixture), cfg)
	wantJSON(t, got, `[{"count_tag":3,"unique_count_tag":2,"sum_amount":22.5,"average_amount":7.5,"count_city":4,"min_amount":"3","max_city":"Paris","appended_tag":["x","y","y"],"concatenated_city":"Berlin|Hamburg|Paris|Paris","concatenated_obj":"{\"a\":1},{\"a\":1}"}]`)
}

func TestSummarizeSplitAndSingleItem(t *testing.T) {
	base := map[string]interface{}{
		"fields_to_summarize": []interface{}{map[string]interface{}{"aggregation": "sum", "field": "amount"}},
		"fields_to_split_by":  "country, city",
	}
	wantJSON(t, run(t, &SummarizeNode{}, items(t, salesFixture), base),
		`[{"sum_amount":10,"country":"DE","city":"Berlin"},{"sum_amount":5,"country":"DE","city":"Hamburg"},{"sum_amount":7.5,"country":"FR","city":"Paris"}]`)

	base["output_format"] = "singleItem"
	wantJSON(t, run(t, &SummarizeNode{}, items(t, salesFixture), base),
		`[{"DE":{"Berlin":{"sum_amount":10},"Hamburg":{"sum_amount":5}},"FR":{"Paris":{"sum_amount":7.5}}}]`)

	skip := map[string]interface{}{
		"fields_to_summarize":     []interface{}{map[string]interface{}{"aggregation": "count", "field": "city"}},
		"fields_to_split_by":      "tag",
		"skip_empty_split_fields": true,
	}
	wantJSON(t, run(t, &SummarizeNode{}, items(t, salesFixture), skip), `[{"count_city":1,"tag":"x"},{"count_city":2,"tag":"y"}]`)
}

func TestSummarizeErrors(t *testing.T) {
	in := workflow.NodeInput{Items: items(t, salesFixture)}
	for _, cfg := range []map[string]interface{}{
		{},
		{"fields_to_summarize": `[{"aggregation":"median","field":"x"}]`},
		{"fields_to_summarize": `[{"aggregation":"sum","field":""}]`},
		{"fields_to_summarize": `not json`},
		{"fields_to_summarize": `[{"aggregation":"sum","field":"amount"}]`, "output_format": "table"},
	} {
		if _, err := (&SummarizeNode{}).Execute(context.Background(), in, cfg); err == nil {
			t.Errorf("config %v: expected error", cfg)
		}
	}
}

func TestRenameKeys(t *testing.T) {
	in := `[{"fname":"Ada","lname":"L","nested":{"old_a":1,"deep":{"old_b":2}},"list":[{"old_c":3}]}]`
	cfg := map[string]interface{}{
		"keys": []interface{}{
			map[string]interface{}{"current_key": "fname", "new_key": "first.name"},
			map[string]interface{}{"currentKey": "missing", "newKey": "x"},
			map[string]interface{}{"current_key": "lname", "new_key": "lname"},
		},
	}
	wantJSON(t, run(t, &RenameKeysNode{}, items(t, in), cfg),
		`[{"first":{"name":"Ada"},"lname":"L","nested":{"old_a":1,"deep":{"old_b":2}},"list":[{"old_c":3}]}]`)

	cfg = map[string]interface{}{"regex_replacements": `[{"search_regex":"^OLD_(\\w)","replace_regex":"new_$1x","case_insensitive":true}]`}
	wantJSON(t, run(t, &RenameKeysNode{}, items(t, in), cfg),
		`[{"fname":"Ada","lname":"L","nested":{"new_ax":1,"deep":{"new_bx":2}},"list":[{"new_cx":3}]}]`)

	cfg = map[string]interface{}{"regex_replacements": []interface{}{map[string]interface{}{"search_regex": "^old_", "replace_regex": "", "options": map[string]interface{}{"depth": 1.0}}}}
	wantJSON(t, run(t, &RenameKeysNode{}, items(t, in), cfg),
		`[{"fname":"Ada","lname":"L","nested":{"a":1,"deep":{"old_b":2}},"list":[{"old_c":3}]}]`)

	if _, err := (&RenameKeysNode{}).Execute(context.Background(), workflow.NodeInput{}, map[string]interface{}{"regex_replacements": `[{"search_regex":"("}]`}); err == nil {
		t.Error("bad regex: expected error")
	}
}

func TestRenameKeysDoesNotMutateInput(t *testing.T) {
	in := items(t, `[{"a":{"b":1}}]`)
	run(t, &RenameKeysNode{}, in, map[string]interface{}{"keys": `[{"current_key":"a.b","new_key":"c"}]`})
	wantJSON(t, in[0].JSON, `{"a":{"b":1}}`)
}
