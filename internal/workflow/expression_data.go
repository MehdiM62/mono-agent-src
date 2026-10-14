package workflow

import (
	"os"
	"reflect"
	"strings"
	"sync"
	"text/template"
	"text/template/parse"
	"time"
)

// parsedTemplate is a cached template plus the costly data it reads.
type parsedTemplate struct {
	t     *template.Template
	needs templateNeeds
}

// templateNeeds records which expensive parts of the template data a
// template can reach, so a template that never mentions $node does not pay
// for building a proxy of every upstream node's output.
type templateNeeds struct {
	node bool // $node, $items
	env  bool // $env
	now  bool // $now, $today
}

// needsOf walks every tree of t and reports the data keys user code
// references. Nodes positioned inside the preamble (its first preLen bytes)
// are skipped: the preamble's declarations are not uses.
func needsOf(t *template.Template, preLen int) templateNeeds {
	var n templateNeeds
	mark := func(key string) {
		switch key {
		case "node", "items":
			n.node = true
		case "env":
			n.env = true
		case "now", "today":
			n.now = true
		case "$":
			n = templateNeeds{node: true, env: true, now: true}
		}
	}
	var walk func(parse.Node)
	walk = func(node parse.Node) {
		switch x := node.(type) {
		case *parse.ListNode:
			if x == nil {
				return
			}
			for _, c := range x.Nodes {
				walk(c)
			}
		case *parse.ActionNode:
			walk(x.Pipe)
		case *parse.IfNode:
			walk(x.Pipe)
			walk(x.List)
			walk(x.ElseList)
		case *parse.RangeNode:
			walk(x.Pipe)
			walk(x.List)
			walk(x.ElseList)
		case *parse.WithNode:
			walk(x.Pipe)
			walk(x.List)
			walk(x.ElseList)
		case *parse.TemplateNode:
			walk(x.Pipe)
		case *parse.PipeNode:
			if x == nil {
				return
			}
			for _, c := range x.Cmds {
				walk(c)
			}
		case *parse.CommandNode:
			for _, a := range x.Args {
				walk(a)
			}
		case *parse.ChainNode:
			walk(x.Node)
		case *parse.VariableNode:
			if int(x.Pos) >= preLen {
				mark(strings.TrimPrefix(x.Ident[0], "$"))
				if x.Ident[0] == "$" {
					mark("$")
				}
			}
		case *parse.FieldNode:
			if int(x.Pos) >= preLen {
				mark(x.Ident[0])
			}
		case *parse.DotNode:
			if int(x.Pos) >= preLen {
				mark("$")
			}
		}
	}
	for _, tt := range t.Templates() {
		if tt.Tree != nil {
			walk(tt.Tree.Root)
		}
	}
	return n
}

// exprTime prints as RFC3339 while keeping time.Time's methods, so
// {{ $now }} renders a timestamp and {{ $now.Format "2006-01-02" }} works.
type exprTime struct{ time.Time }

func (t exprTime) String() string { return t.Format(time.RFC3339) }

// exprBase is the part of a template's data derived from everything in an
// ExpressionContext except JSON. A prepared context (Prepared/WithJSON)
// shares one, so the $node proxies and the $env map are built once per node
// execution instead of once per evaluated string.
type exprBase struct {
	// The inputs the base was built from, checked before reuse so a copy
	// whose Node or Env was swapped never sees stale data.
	srcNode map[string][]Item
	srcEnv  map[string]string

	now, today exprTime

	nodeOnce sync.Once
	nodes    map[string]interface{}
	items    map[string]interface{}

	envOnce sync.Once
	env     map[string]string
}

func newExprBase(ctx ExpressionContext) *exprBase {
	loc := ctx.Location
	if loc == nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	y, m, d := now.Date()
	return &exprBase{
		srcNode: ctx.Node,
		srcEnv:  ctx.Env,
		now:     exprTime{now},
		today:   exprTime{time.Date(y, m, d, 0, 0, 0, 0, loc)},
	}
}

func (b *exprBase) matches(ctx ExpressionContext) bool {
	return sameMap(b.srcNode, ctx.Node) && sameMap(b.srcEnv, ctx.Env)
}

// sameMap reports whether a and b are the same map value (not merely equal
// contents) of the same length.
func sameMap(a, b interface{}) bool {
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	return va.UnsafePointer() == vb.UnsafePointer() && va.Len() == vb.Len()
}

// nodeData returns the $node proxies and the $items lists. Each proxy is
// {"json": first item, "all": every item, "items": every item}, so
// $node["Name"].json.key keeps working next to $node["Name"].all.
func (b *exprBase) nodeData() (map[string]interface{}, map[string]interface{}) {
	b.nodeOnce.Do(func() {
		b.nodes = make(map[string]interface{}, len(b.srcNode))
		b.items = make(map[string]interface{}, len(b.srcNode))
		for name, items := range b.srcNode {
			all := make([]interface{}, len(items))
			for i, it := range items {
				j := it.JSON
				if j == nil {
					j = map[string]interface{}{}
				}
				all[i] = j
			}
			var first interface{} = map[string]interface{}{}
			if len(all) > 0 {
				first = all[0]
			}
			b.nodes[name] = nodeJSONProxy{"json": first, "all": all, "items": all}
			b.items[name] = all
		}
	})
	return b.nodes, b.items
}

// envData returns the $env map: ONLY the variables explicitly provided via
// ctx.Env. The full OS environment is merged in exclusively when
// MONOAGENT_ALLOW_ENV_TEMPLATES=1: by default a template must not be able
// to read the invoking shell's environment (which typically holds API keys
// and other secrets). Values are strings so that a missing key renders as
// the empty string rather than "<no value>".
func (b *exprBase) envData() map[string]string {
	b.envOnce.Do(func() {
		b.env = make(map[string]string, len(b.srcEnv))
		if os.Getenv("MONOAGENT_ALLOW_ENV_TEMPLATES") == "1" {
			for _, kv := range os.Environ() {
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) == 2 {
					b.env[parts[0]] = parts[1]
				}
			}
		}
		for k, v := range b.srcEnv {
			b.env[k] = v
		}
	})
	return b.env
}

// Prepared returns a copy of ctx that caches its derived template data
// ($node proxies, $env, $now) for every evaluation made with it or with a
// WithJSON copy of it. Prepare once per node execution, then vary JSON per
// item. Do not modify the Node or Env maps of a prepared context in place.
func (ctx ExpressionContext) Prepared() ExpressionContext {
	ctx.base = newExprBase(ctx)
	return ctx
}

// WithJSON returns a copy of ctx with JSON replaced, keeping any prepared
// data, so per-item evaluation does not rebuild the per-node parts.
func (ctx ExpressionContext) WithJSON(j map[string]interface{}) ExpressionContext {
	ctx.JSON = j
	return ctx
}

// buildData converts an ExpressionContext into the map passed as dot to
// templates, building only the costly parts the template needs.
func buildData(ctx ExpressionContext, needs templateNeeds) map[string]interface{} {
	b := ctx.base
	if (needs.node || needs.env || needs.now) && (b == nil || !b.matches(ctx)) {
		b = newExprBase(ctx)
	}
	jsonMap := ctx.JSON
	if jsonMap == nil {
		jsonMap = make(map[string]interface{})
	}
	data := map[string]interface{}{
		"json":      jsonMap,
		"workflow":  map[string]interface{}{"id": ctx.WorkflowID},
		"execution": map[string]interface{}{"id": ctx.ExecutionID},
		"trigger":   ctx.Trigger,
		"vars":      ctx.Vars,
		"runIndex":  ctx.RunIndex,
		"prevNode":  ctx.PrevNode,
	}
	if needs.node {
		data["node"], data["items"] = b.nodeData()
	}
	if needs.env {
		data["env"] = b.envData()
	}
	if needs.now {
		data["now"], data["today"] = b.now, b.today
	}
	return data
}
