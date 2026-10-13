package workflow

import (
	"strings"
	"testing"
	"time"
)

func evalOK(t *testing.T, tmpl string, ctx ExpressionContext) string {
	t.Helper()
	got, err := NewExpressionEngine().EvaluateString(tmpl, ctx)
	if err != nil {
		t.Fatalf("EvaluateString(%q): %v", tmpl, err)
	}
	return got
}

func twoItemNodes() map[string][]Item {
	return map[string][]Item{
		"Fetch": {
			NewItem(map[string]interface{}{"id": "a"}),
			NewItem(map[string]interface{}{"id": "b"}),
		},
	}
}

func TestExpressionItemsVariable(t *testing.T) {
	ctx := ExpressionContext{Node: twoItemNodes()}
	cases := map[string]string{
		`{{ len $items["Fetch"] }}`:                               "2",
		`{{ (index $items["Fetch"] 1).id }}`:                      "b",
		`{{ range $items["Fetch"] }}{{ .id }},{{ end }}`:          "a,b,",
		`{{ if $items["Missing"] }}x{{ else }}none{{ end }}`:      "none",
		`{{ range $i, $it := $items["Fetch"] }}{{ $i }}{{ end }}`: "01",
	}
	for tmpl, want := range cases {
		if got := evalOK(t, tmpl, ctx); got != want {
			t.Errorf("%s = %q, want %q", tmpl, got, want)
		}
	}
}

func TestExpressionNodeAllAndItems(t *testing.T) {
	ctx := ExpressionContext{Node: twoItemNodes()}
	cases := map[string]string{
		`{{ len $node["Fetch"].all }}`:            "2",
		`{{ (index $node["Fetch"].items 1).id }}`: "b",
		`{{ $node["Fetch"].json.id }}`:            "a",
	}
	for tmpl, want := range cases {
		if got := evalOK(t, tmpl, ctx); got != want {
			t.Errorf("%s = %q, want %q", tmpl, got, want)
		}
	}
}

func TestExpressionNowAndToday(t *testing.T) {
	before := time.Now().UTC().Add(-time.Second)
	got := evalOK(t, `{{ $now }}`, ExpressionContext{})
	now, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("$now %q is not RFC3339: %v", got, err)
	}
	if now.Before(before.Truncate(time.Second)) || now.After(time.Now().UTC().Add(time.Second)) {
		t.Errorf("$now %v is not the current time", now)
	}
	if got := evalOK(t, `{{ $now.Format "2006" }}`, ExpressionContext{}); got != now.Format("2006") {
		t.Errorf("$now.Format = %q", got)
	}
	today := evalOK(t, `{{ $today }}`, ExpressionContext{})
	if want := now.Format("2006-01-02") + "T00:00:00Z"; today != want {
		t.Errorf("$today = %q, want %q", today, want)
	}
}

func TestExpressionTriggerVarsRunIndexPrevNode(t *testing.T) {
	ctx := ExpressionContext{
		Trigger:  map[string]interface{}{"url": "https://x"},
		Vars:     map[string]interface{}{"region": "eu"},
		RunIndex: 3,
		PrevNode: "Fetch",
	}
	cases := map[string]string{
		`{{ $trigger.url }}`:    "https://x",
		`{{ $vars.region }}`:    "eu",
		`{{ $vars.missing }}`:   "",
		`{{ $runIndex }}`:       "3",
		`{{ $prevNode }}`:       "Fetch",
		`{{ $trigger.absent }}`: "",
	}
	for tmpl, want := range cases {
		if got := evalOK(t, tmpl, ctx); got != want {
			t.Errorf("%s = %q, want %q", tmpl, got, want)
		}
	}
	// Unset optional fields render empty / zero, never "<no value>".
	empty := ExpressionContext{}
	for tmpl, want := range map[string]string{`{{ $trigger.url }}`: "", `{{ $vars.x }}`: "", `{{ $runIndex }}`: "0", `{{ $prevNode }}`: ""} {
		if got := evalOK(t, tmpl, empty); got != want {
			t.Errorf("empty ctx %s = %q, want %q", tmpl, got, want)
		}
	}
}

func TestTemplateNeedsOnlyWhatIsReferenced(t *testing.T) {
	e := NewExpressionEngine()
	cases := []struct {
		tmpl      string
		node, env bool
	}{
		{`hi {{ $json.name }}`, false, false},
		{`{{ $node["X"].json.a }}`, true, false},
		{`{{ .node }}`, true, false},
		{`{{ $env.HOME }}`, false, true},
		{`{{ len $items["X"] }}`, true, false},
		// Bare dot exposes everything.
		{`{{ json . }}`, true, true},
	}
	for _, c := range cases {
		p, err := e.parse(c.tmpl)
		if err != nil {
			t.Fatalf("parse %q: %v", c.tmpl, err)
		}
		if p.needs.node != c.node || p.needs.env != c.env {
			t.Errorf("%q needs = %+v, want node=%v env=%v", c.tmpl, p.needs, c.node, c.env)
		}
	}
}

func TestPreparedContextReusesAndRevalidates(t *testing.T) {
	e := NewExpressionEngine()
	ctx := ExpressionContext{Node: twoItemNodes()}.Prepared()
	item := ctx.WithJSON(map[string]interface{}{"k": "v"})
	got, err := e.EvaluateString(`{{ $json.k }} {{ $node["Fetch"].json.id }}`, item)
	if err != nil || got != "v a" {
		t.Fatalf("prepared eval = %q, %v", got, err)
	}
	if item.base != ctx.base {
		t.Error("WithJSON dropped the prepared base")
	}
	// Swapping Node on a prepared copy must not serve the stale proxies.
	stale := item
	stale.Node = map[string][]Item{"Fetch": {NewItem(map[string]interface{}{"id": "z"})}}
	if got := evalOK(t, `{{ $node["Fetch"].json.id }}`, stale); got != "z" {
		t.Errorf("stale base served: got %q, want z", got)
	}
}

func TestResolveConfigNodeAcrossFields(t *testing.T) {
	ctx := ExpressionContext{Node: twoItemNodes(), JSON: map[string]interface{}{"n": "x"}}
	out, err := NewExpressionEngine().ResolveConfig(map[string]interface{}{
		"a": `{{ $node["Fetch"].json.id }}`,
		"b": map[string]interface{}{"c": `{{ len $items["Fetch"] }}-{{ $json.n }}`},
	}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if out["a"] != "a" || out["b"].(map[string]interface{})["c"] != "2-x" {
		t.Errorf("ResolveConfig = %v", out)
	}
}

func TestExpressionNowConsistentWithinPreparedContext(t *testing.T) {
	ctx := ExpressionContext{}.Prepared()
	a := evalOK(t, `{{ $now.UnixNano }}`, ctx)
	b := evalOK(t, `{{ $now.UnixNano }}`, ctx)
	if a != b || strings.TrimSpace(a) == "" {
		t.Errorf("$now changed within one prepared context: %s vs %s", a, b)
	}
}

// Leading whitespace was always trimmed by the preamble; it still is when a
// template mentions no preamble variable at all.
func TestExpressionLeadingWhitespaceTrimmedAsBefore(t *testing.T) {
	ctx := ExpressionContext{JSON: map[string]interface{}{"a": 1}}
	for _, tmpl := range []string{"  {{ .json.a }}", "  {{ $json.a }}"} {
		if got := evalOK(t, tmpl, ctx); got != "1" {
			t.Errorf("%q = %q, want %q", tmpl, got, "1")
		}
	}
}
