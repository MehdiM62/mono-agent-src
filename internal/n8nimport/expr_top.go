package n8nimport

import "strings"

// top emits n as template text: literal text stays text, concatenation
// and ternaries become a sequence of actions, anything else one action.
func (e *emitter) top(n *jsNode) (string, error) {
	switch n.kind {
	case jsStr:
		return escapeTemplateText(n.str), nil
	case jsTemplate:
		return e.topSeq(templateOperands(n))
	case jsCond:
		c, err := e.cond(n.args[0])
		if err != nil {
			return "", err
		}
		a, err := e.top(n.args[1])
		if err != nil {
			return "", err
		}
		b, err := e.top(n.args[2])
		if err != nil {
			return "", err
		}
		return "{{ if " + stripParens(c) + " }}" + a + "{{ else }}" + b + "{{ end }}", nil
	case jsBinary:
		if n.name == "+" {
			nodes := plusOperands(n)
			ops, err := e.typesOnly(nodes)
			if err == nil && e.isConcat(ops) {
				return e.topSeq(nodes)
			}
		}
	}
	v, err := e.value(n)
	if err != nil {
		return "", err
	}
	if v.strLit {
		return escapeTemplateText(v.str), nil
	}
	return "{{ " + stripParens(v.p) + " }}", nil
}

// typesOnly emits operands to learn their types; ternary operands count as
// unknown (they are emitted as blocks by topSeq).
func (e *emitter) typesOnly(nodes []*jsNode) ([]val, error) {
	saved := e.notes
	defer func() { e.notes = saved }()
	out := make([]val, 0, len(nodes))
	for _, n := range nodes {
		if n.kind == jsCond {
			out = append(out, val{t: tAny})
			continue
		}
		v, err := e.value(n)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (e *emitter) topSeq(nodes []*jsNode) (string, error) {
	var b strings.Builder
	for _, n := range nodes {
		s, err := e.top(n)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	return b.String(), nil
}

// cond emits n for use as a condition ({{ if … }}, core.if, core.filter).
func (e *emitter) cond(n *jsNode) (string, error) {
	v, err := e.value(n)
	if err != nil {
		return "", err
	}
	return v.p, nil
}

// stripParens removes one pair of parentheses enclosing the whole operand.
func stripParens(p string) string {
	if len(p) < 2 || p[0] != '(' || p[len(p)-1] != ')' {
		return p
	}
	depth := 0
	inStr := false
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case inStr && c == '\\':
			i++
		case c == '"':
			inStr = !inStr
		case !inStr && c == '(':
			depth++
		case !inStr && c == ')':
			depth--
			if depth == 0 && i != len(p)-1 {
				return p
			}
		}
	}
	return p[1 : len(p)-1]
}

// escapeTemplateText makes literal text safe inside a mono template: "{{"
// would otherwise start an action.
func escapeTemplateText(s string) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	return strings.ReplaceAll(s, "{{", `{{"{{"}}`)
}
