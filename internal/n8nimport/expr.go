package n8nimport

import (
	"strconv"
	"strings"
)

// ExprFailure is an n8n expression that could not be translated into
// mono's template language. The original expression is kept in the node
// config so it fails loudly at run time instead of silently doing
// something else.
type ExprFailure struct {
	Expression string `json:"expression"`
	Reason     string `json:"reason"`
}

// exprTranslator translates n8n parameter values ("=…{{ js }}…") into mono
// template strings ("…{{ go-template }}…").
type exprTranslator struct {
	workflowName string
	notes        []string
	failures     []ExprFailure
}

func (t *exprTranslator) emitter() *emitter { return &emitter{workflowName: t.workflowName} }

func (t *exprTranslator) absorb(e *emitter) {
	for _, n := range e.notes {
		t.addNote(n)
	}
}

func (t *exprTranslator) addNote(n string) {
	for _, x := range t.notes {
		if x == n {
			return
		}
	}
	t.notes = append(t.notes, n)
}

func (t *exprTranslator) fail(expr string, err error) {
	t.failures = append(t.failures, ExprFailure{Expression: expr, Reason: err.Error()})
}

// String translates one parameter string. A plain string is returned with
// any "{{" escaped; an expression ("=…") is translated, or returned without
// its "=" marker (and recorded as a failure) when it cannot be.
func (t *exprTranslator) String(s string) (string, bool) {
	if !strings.HasPrefix(s, "=") {
		return escapeTemplateText(s), true
	}
	body := s[1:]
	parts, err := splitTemplate(body)
	if err != nil {
		t.fail(body, err)
		return body, false
	}
	e := t.emitter()
	var b strings.Builder
	for _, p := range parts {
		if !p.isCode {
			b.WriteString(escapeTemplateText(p.text))
			continue
		}
		if strings.TrimSpace(p.code) == "" {
			continue
		}
		n, err := parseJS(p.code)
		if err != nil {
			t.fail(body, err)
			return body, false
		}
		out, err := e.top(normalizeJS(n))
		if err != nil {
			t.fail(body, err)
			return body, false
		}
		b.WriteString(out)
	}
	t.absorb(e)
	return b.String(), true
}

// Value translates a parameter value of any shape: strings as above,
// maps and slices recursively. Resource locators ({"__rl": true, "value":
// …}) collapse to their value.
func (t *exprTranslator) Value(v interface{}) interface{} {
	switch x := v.(type) {
	case string:
		s, _ := t.String(x)
		return s
	case map[string]interface{}:
		if rl, ok := x["__rl"].(bool); ok && rl {
			return t.Value(x["value"])
		}
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			out[k] = t.Value(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, val := range x {
			out[i] = t.Value(val)
		}
		return out
	}
	return v
}

// operand translates a value into a template operand (for conditions).
func (t *exprTranslator) operand(v interface{}) (val, bool) {
	switch x := v.(type) {
	case nil:
		return strVal(""), true
	case bool:
		return val{p: strconv.FormatBool(x), t: tBool}, true
	case float64:
		return numVal(x), true
	case int:
		return numVal(float64(x)), true
	case map[string]interface{}:
		if rl, ok := x["__rl"].(bool); ok && rl {
			return t.operand(x["value"])
		}
		return val{}, false
	case string:
		if !strings.HasPrefix(x, "=") {
			return strVal(x), true
		}
		body := x[1:]
		parts, err := splitTemplate(body)
		if err != nil {
			t.fail(body, err)
			return val{}, false
		}
		var nodes []*jsNode
		for _, p := range parts {
			if !p.isCode {
				nodes = append(nodes, &jsNode{kind: jsStr, str: p.text})
				continue
			}
			if strings.TrimSpace(p.code) == "" {
				continue
			}
			n, err := parseJS(p.code)
			if err != nil {
				t.fail(body, err)
				return val{}, false
			}
			nodes = append(nodes, normalizeJS(n))
		}
		e := t.emitter()
		var out val
		switch len(nodes) {
		case 0:
			out = strVal("")
		case 1:
			out, err = e.value(nodes[0])
		default:
			out, err = e.concat(nodes)
		}
		if err != nil {
			t.fail(body, err)
			return val{}, false
		}
		t.absorb(e)
		return out, true
	}
	return val{}, false
}

// normalizeJS rewrites n8n helper calls into plain JavaScript forms the
// emitter understands: $if(c, a, b) becomes c ? a : b.
func normalizeJS(n *jsNode) *jsNode {
	if n == nil {
		return nil
	}
	for i, a := range n.args {
		n.args[i] = normalizeJS(a)
	}
	if n.kind == jsCall && n.args[0].kind == jsIdent && n.args[0].name == "$if" && len(n.args) == 4 {
		return &jsNode{kind: jsCond, args: n.args[1:]}
	}
	return n
}
