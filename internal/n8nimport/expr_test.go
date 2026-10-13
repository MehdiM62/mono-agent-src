package n8nimport

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// exprCtx is the item and node outputs every expression case runs against.
func exprCtx() workflow.ExpressionContext {
	return workflow.ExpressionContext{
		JSON: map[string]interface{}{
			"name":     "Ada Lovelace",
			"first":    "Ada",
			"last":     "Lovelace",
			"email":    "ADA@Example.com ",
			"count":    float64(41),
			"price":    float64(9.5),
			"qty":      "3",
			"active":   true,
			"status":   "open",
			"tags":     []interface{}{"x", "y"},
			"user":     map[string]interface{}{"id": float64(7), "profile": map[string]interface{}{"city": "London"}},
			"first na": "spaced",
			"items":    []interface{}{map[string]interface{}{"sku": "A1"}},
			"empty":    "",
			"csv":      "a,b,c",
			"url":      "https://example.com/path",
			"nothing":  nil,
		},
		Node: map[string][]workflow.Item{
			"HTTP Request": {{JSON: map[string]interface{}{"body": map[string]interface{}{"total": float64(3)}, "statusCode": float64(200)}}},
			"Get User":     {{JSON: map[string]interface{}{"id": "u-1", "name": "Grace"}}},
		},
		WorkflowID:  "wf-1",
		ExecutionID: "ex-9",
	}
}

func TestTranslateExpressions(t *testing.T) {
	cases := []struct {
		in   string
		want string // evaluated result; "" with skip=true means only check it translates
		eval bool
	}{
		// plain values and field access
		{"plain text", "plain text", true},
		{"={{ $json.name }}", "Ada Lovelace", true},
		{"={{$json.name}}", "Ada Lovelace", true},
		{"={{ $json['name'] }}", "Ada Lovelace", true},
		{`={{ $json["first na"] }}`, "spaced", true},
		{"={{ $json.user.profile.city }}", "London", true},
		{"={{ $json.user['profile'].city }}", "London", true},
		{"={{ $json.items[0].sku }}", "A1", true},
		{"={{ $json.tags[1] }}", "y", true},
		{"={{ $json.user.id }}", "7", true},
		{"={{ $data.first }}", "Ada", true},
		{"=Hello {{ $json.first }}!", "Hello Ada!", true},
		{"={{ $json.first }} {{ $json.last }}", "Ada Lovelace", true},
		{"=static only", "static only", true},
		{"={{ 'literal' }}", "literal", true},
		{"={{ 42 }}", "42", true},
		// other nodes
		{"={{ $('Get User').item.json.name }}", "Grace", true},
		{"={{ $('Get User').first().json.id }}", "u-1", true},
		{"={{ $('Get User').last().json.id }}", "u-1", true},
		{`={{ $node["HTTP Request"].json.body.total }}`, "3", true},
		{`={{ $node['Get User'].json["name"] }}`, "Grace", true},
		{`={{ $items("Get User")[0].json.name }}`, "Grace", true},
		{"={{ $('HTTP Request').item.json.statusCode }}", "200", true},
		{"={{ $input.item.json.first }}", "Ada", true},
		{"={{ $input.first().json.status }}", "open", true},
		// concatenation
		{"={{ $json.first + ' ' + $json.last }}", "Ada Lovelace", true},
		{"={{ 'Dear ' + $json.first }}", "Dear Ada", true},
		{"={{ `Hi ${$json.first}, you have ${$json.count} items` }}", "Hi Ada, you have 41 items", true},
		{"={{ ($json.first + '-' + $json.last).toLowerCase() }}", "ada-lovelace", true},
		{"={{ 'x' + 'y' }}", "xy", true},
		{"={{ $json.first + $json.last }}", "AdaLovelace", true},
		// arithmetic
		{"={{ $json.count + 1 }}", "42", true},
		{"={{ $json.price * 2 }}", "19", true},
		{"={{ $json.count - 1 }}", "40", true},
		{"={{ $json.count / 2 }}", "20.5", true},
		{"={{ Number($json.qty) + 1 }}", "4", true},
		{"={{ parseInt($json.qty) }}", "3", true},
		{"={{ parseFloat('2.5') * 2 }}", "5", true},
		{"={{ 1 + 2 }}", "3", true},
		{"={{ $json.price.toFixed(2) }}", "9.50", true},
		// comparisons and ternaries
		{"={{ $json.count > 40 }}", "true", true},
		{"={{ $json.count >= 42 }}", "false", true},
		{"={{ $json.status === 'open' }}", "true", true},
		{"={{ $json.status !== 'open' }}", "false", true},
		{"={{ $json.count == 41 }}", "true", true},
		{"={{ $json.active === true }}", "true", true},
		{"={{ $json.active ? 'yes' : 'no' }}", "yes", true},
		{"={{ $json.count > 100 ? 'big' : 'small' }}", "small", true},
		{"={{ $json.status === 'open' && $json.count > 10 }}", "true", true},
		{"={{ $json.status === 'closed' || $json.active }}", "true", true},
		{"={{ !$json.active }}", "false", true},
		{"={{ $json.missing ?? 'fallback' }}", "fallback", true},
		{"={{ $json.empty || 'default' }}", "default", true},
		{"={{ $json.nothing === null }}", "true", true},
		{"={{ $json.name != null }}", "true", true},
		{"={{ $if($json.active, 'on', 'off') }}", "on", true},
		{"={{ $ifEmpty($json.empty, 'none') }}", "none", true},
		{"=Status: {{ $json.active ? 'active' : 'inactive' }}", "Status: active", true},
		// string methods
		{"={{ $json.name.toUpperCase() }}", "ADA LOVELACE", true},
		{"={{ $json.email.trim().toLowerCase() }}", "ada@example.com", true},
		{"={{ $json.name.length }}", "12", true},
		{"={{ $json.tags.length }}", "2", true},
		{"={{ $json.name.includes('Love') }}", "true", true},
		{"={{ $json.name.startsWith('Ada') }}", "true", true},
		{"={{ $json.name.endsWith('lace') }}", "true", true},
		{"={{ $json.name.endsWith('Ada') }}", "false", true},
		{"={{ $json.name.replace('Ada', 'Augusta') }}", "Augusta Lovelace", true},
		{"={{ $json.csv.replaceAll(',', ';') }}", "a;b;c", true},
		{"={{ $json.csv.replace(/,/g, ' | ') }}", "a | b | c", true},
		{"={{ $json.csv.split(',').join('-') }}", "a-b-c", true},
		{"={{ $json.name.substring(0, 3) }}", "Ada", true},
		{"={{ $json.name.slice(4) }}", "Lovelace", true},
		{"={{ String($json.count) }}", "41", true},
		{"={{ $json.count.toString() }}", "41", true},
		{"={{ JSON.stringify($json.user.profile) }}", `{"city":"London"}`, true},
		{"={{ JSON.parse('{\"a\":1}').a }}", "1", true},
		{"={{ encodeURIComponent($json.first) }}", "Ada", true},
		{"={{ $json.empty.isEmpty() }}", "true", true},
		{"={{ $json.qty.toNumber() + 1 }}", "4", true},
		// workflow and execution
		{"={{ $workflow.id }}", "wf-1", true},
		{"={{ $workflow.name }}", "My Flow", true},
		{"={{ $execution.id }}", "ex-9", true},
		{"={{ $runIndex }}", "0", true},
		// dates: only shape-checked (the value changes)
		{"={{ $now }}", "", false},
		{"={{ $now.toISO() }}", "", false},
		{"={{ $today.toISODate() }}", "", false},
		{"={{ $now.toFormat('yyyy-MM-dd') }}", "", false},
		{"={{ $now.toFormat('dd/MM/yyyy HH:mm') }}", "", false},
		{"={{ DateTime.now().toFormat('yyyy') }}", "", false},
		{"={{ new Date().toISOString() }}", "", false},
		{"={{ $now.year }}", "", false},
		{"={{ $env.API_BASE }}", "", false},
		{"=literal {{ '{{' }} braces", "literal {{ braces", true},
	}
	eng := workflow.NewExpressionEngine()
	for _, c := range cases {
		tr := &exprTranslator{workflowName: "My Flow"}
		out, ok := tr.String(c.in)
		if !ok {
			t.Errorf("%s: not translated: %+v", c.in, tr.failures)
			continue
		}
		got, err := eng.EvaluateString(out, exprCtx())
		if err != nil {
			t.Errorf("%s → %s: evaluate: %v", c.in, out, err)
			continue
		}
		if c.eval && got != c.want {
			t.Errorf("%s → %s: got %q, want %q", c.in, out, got, c.want)
		}
	}
}

func TestDateFormatShapes(t *testing.T) {
	eng := workflow.NewExpressionEngine()
	for in, wantLen := range map[string]int{
		"={{ $now.toFormat('yyyy-MM-dd') }}":         10,
		"={{ $today.toFormat('yyyy-MM-dd HH:mm') }}": 16,
		"={{ $today }}": 20,
		"={{ $now }}":   20,
	} {
		tr := &exprTranslator{}
		out, ok := tr.String(in)
		if !ok {
			t.Fatalf("%s not translated", in)
		}
		got, err := eng.EvaluateString(out, exprCtx())
		if err != nil || len(got) != wantLen {
			t.Errorf("%s → %s = %q (%v), want length %d", in, out, got, err, wantLen)
		}
		if strings.Contains(in, "$today") && !strings.Contains(got, "00:00") {
			t.Errorf("%s = %q, want midnight", in, got)
		}
	}
}

func TestUntranslatableExpressionsAreKept(t *testing.T) {
	for _, in := range []string{
		"={{ $json.items.map(i => i.sku).join(', ') }}",
		"={{ $input.all().length }}",
		"={{ $json.name.match(/[A-Z]+/)[0] }}",
		"={{ Math.round($json.price) }}",
		"={{ $now.minus({days: 7}).toISO() }}",
		"={{ $json.tags.filter(t => t !== 'x') }}",
		"={{ $('Get User').all().length }}",
		"={{ $itemIndex }}",
		"={{ $binary.data.fileName }}",
		"={{ (() => { return 1 })() }}",
		"={{ $json.a ? ($json.b ? 1 : 2) + 1 : 3 }}",
		"={{ typeof $json.x === 'string' }}",
		"={{ $json.name.replace(/ada/i, 'x') }}",
		"={{ $json.x % 2 }}",
		"={{ $prevNode.name }}",
		"={{ [1, 2, 3] }}",
		"={{ $json.name.padStart(10) }}",
		"={{ $node[\"Loop\"].context[\"noItemsLeft\"] }}",
		"={{ $json.a ",
	} {
		tr := &exprTranslator{}
		out, ok := tr.String(in)
		if ok {
			t.Errorf("%s: translated to %q, want it kept", in, out)
			continue
		}
		if out != in[1:] {
			t.Errorf("%s: kept as %q, want the original without '='", in, out)
		}
		if len(tr.failures) != 1 || tr.failures[0].Reason == "" {
			t.Errorf("%s: failures = %+v", in, tr.failures)
		}
	}
}

func TestPlainStringsEscapeBraces(t *testing.T) {
	tr := &exprTranslator{}
	out, ok := tr.String("Use {{name}} placeholders")
	if !ok {
		t.Fatal("plain string not ok")
	}
	got, err := workflow.NewExpressionEngine().EvaluateString(out, exprCtx())
	if err != nil || got != "Use {{name}} placeholders" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestValueCollapsesResourceLocators(t *testing.T) {
	tr := &exprTranslator{}
	got := tr.Value(map[string]interface{}{
		"channel": map[string]interface{}{"__rl": true, "value": "C123", "mode": "id"},
		"list":    []interface{}{"={{ $json.first }}", 3.0},
	}).(map[string]interface{})
	if got["channel"] != "C123" {
		t.Errorf("channel = %v", got["channel"])
	}
	if l := got["list"].([]interface{}); l[0] != "{{ $json.first }}" || l[1] != 3.0 {
		t.Errorf("list = %v", l)
	}
}
