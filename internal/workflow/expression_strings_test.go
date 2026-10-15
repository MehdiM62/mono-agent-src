package workflow

import "testing"

func TestExpressionStringMatchFuncs(t *testing.T) {
	e := NewExpressionEngine()
	ctx := ExpressionContext{JSON: map[string]interface{}{"title": "Hello Go World", "n": 42}}
	for _, tc := range []struct{ tmpl, want string }{
		{`{{ contains "Go" $json.title }}`, "true"},
		{`{{ $json.title | contains "go" }}`, "false"},
		{`{{ startsWith "Hello" $json.title }}`, "true"},
		{`{{ endsWith "World" $json.title }}`, "true"},
		{`{{ endsWith "x" $json.missing }}`, "false"},
		{`{{ matches "^H.*d$" $json.title }}`, "true"},
		{`{{ $json.n | matches "^4[0-9]$" }}`, "true"},
		{`{{ if contains "Go" $json.title }}yes{{ end }}`, "yes"},
	} {
		got, err := e.EvaluateString(tc.tmpl, ctx)
		if err != nil || got != tc.want {
			t.Errorf("%s = %q, %v; want %q", tc.tmpl, got, err, tc.want)
		}
	}
	if _, err := e.EvaluateString(`{{ matches "(" $json.title }}`, ctx); err == nil {
		t.Error("an invalid pattern should fail")
	}
}
