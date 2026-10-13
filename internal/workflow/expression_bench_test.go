package workflow

import (
	"fmt"
	"testing"
)

// BenchmarkExpressionEngine_Simple measures evaluating a single top-level
// field reference, e.g. {{ $json.field }}.
func BenchmarkExpressionEngine_Simple(b *testing.B) {
	engine := NewExpressionEngine()
	ctx := ExpressionContext{
		JSON: map[string]interface{}{"field": "value"},
	}
	tmpl := `{{ $json.field }}`
	// Warm the parse cache so the loop measures execution, not first-parse.
	if _, err := engine.EvaluateString(tmpl, ctx); err != nil {
		b.Fatalf("warmup EvaluateString: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.EvaluateString(tmpl, ctx); err != nil {
			b.Fatalf("EvaluateString: %v", err)
		}
	}
}

// BenchmarkExpressionEngine_Nested measures evaluating a nested field
// reference chained through several map levels, e.g. {{ $json.a.b.c }}.
func BenchmarkExpressionEngine_Nested(b *testing.B) {
	engine := NewExpressionEngine()
	ctx := ExpressionContext{
		JSON: map[string]interface{}{
			"a": map[string]interface{}{
				"b": map[string]interface{}{
					"c": "value",
				},
			},
		},
	}
	tmpl := `{{ $json.a.b.c }}`
	if _, err := engine.EvaluateString(tmpl, ctx); err != nil {
		b.Fatalf("warmup EvaluateString: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.EvaluateString(tmpl, ctx); err != nil {
			b.Fatalf("EvaluateString: %v", err)
		}
	}
}

// BenchmarkExpressionEngine_FallbackChain measures a longer expression that
// combines a node-output lookup with a default fallback, representative of
// expressions seen in real workflow configs.
func BenchmarkExpressionEngine_FallbackChain(b *testing.B) {
	engine := NewExpressionEngine()
	ctx := ExpressionContext{
		JSON: map[string]interface{}{"a": map[string]interface{}{"b": map[string]interface{}{"c": ""}}},
		Node: map[string][]Item{
			"Previous": {{JSON: map[string]interface{}{"fallback": "default-value"}}},
		},
	}
	tmpl := `{{ if $json.a.b.c }}{{ $json.a.b.c }}{{ else }}{{ $node["Previous"].json.fallback }}{{ end }}`
	if _, err := engine.EvaluateString(tmpl, ctx); err != nil {
		b.Fatalf("warmup EvaluateString: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.EvaluateString(tmpl, ctx); err != nil {
			b.Fatalf("EvaluateString: %v", err)
		}
	}
}

// BenchmarkExpressionEngine_50Nodes measures a template that ignores $node
// while 50 upstream node outputs are in scope — the cost of $node must not
// be paid by templates that never reference it.
func BenchmarkExpressionEngine_50Nodes(b *testing.B) {
	engine := NewExpressionEngine()
	nodes := make(map[string][]Item, 50)
	for i := 0; i < 50; i++ {
		nodes[fmt.Sprintf("N%d", i)] = []Item{NewItem(map[string]interface{}{"a": i})}
	}
	ctx := ExpressionContext{JSON: map[string]interface{}{"name": "x"}, Node: nodes}
	tmpl := `hi {{ $json.name }}`
	if _, err := engine.EvaluateString(tmpl, ctx); err != nil {
		b.Fatalf("warmup EvaluateString: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.EvaluateString(tmpl, ctx); err != nil {
			b.Fatalf("EvaluateString: %v", err)
		}
	}
}

// BenchmarkExpressionEngine_ZeroNodes is the 0-node baseline for
// BenchmarkExpressionEngine_50Nodes.
func BenchmarkExpressionEngine_ZeroNodes(b *testing.B) {
	engine := NewExpressionEngine()
	ctx := ExpressionContext{JSON: map[string]interface{}{"name": "x"}}
	tmpl := `hi {{ $json.name }}`
	if _, err := engine.EvaluateString(tmpl, ctx); err != nil {
		b.Fatalf("warmup EvaluateString: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.EvaluateString(tmpl, ctx); err != nil {
			b.Fatalf("EvaluateString: %v", err)
		}
	}
}

// BenchmarkExpressionEngine_ResolveConfig50Nodes resolves a 10-field config
// that reads $node, with 50 upstream nodes — the per-node-execution path.
func BenchmarkExpressionEngine_ResolveConfig50Nodes(b *testing.B) {
	engine := NewExpressionEngine()
	nodes := make(map[string][]Item, 50)
	for i := 0; i < 50; i++ {
		nodes[fmt.Sprintf("N%d", i)] = []Item{NewItem(map[string]interface{}{"a": i})}
	}
	ctx := ExpressionContext{JSON: map[string]interface{}{"name": "x"}, Node: nodes}
	config := make(map[string]interface{}, 10)
	for i := 0; i < 10; i++ {
		config[fmt.Sprintf("f%d", i)] = fmt.Sprintf(`{{ $node["N%d"].json.a }}`, i)
	}
	if _, err := engine.ResolveConfig(config, ctx); err != nil {
		b.Fatalf("warmup ResolveConfig: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.ResolveConfig(config, ctx); err != nil {
			b.Fatalf("ResolveConfig: %v", err)
		}
	}
}
