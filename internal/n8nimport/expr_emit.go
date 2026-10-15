package n8nimport

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// vtype is the static type the emitter knows a value has.
type vtype int

const (
	tAny vtype = iota
	tStr
	tNum
	tBool
)

// objKind marks values that are n8n objects with no direct mono value
// (node references, $input, Luxon dates). They become a value only through
// a member or method that the emitter understands.
type objKind int

const (
	objNone      objKind = iota
	objNodeRef           // $('X'): needs .item/.first()/…
	objNodeItem          // $('X').item / $node["X"]: needs .json
	objItemsList         // $items("X"): needs [0]
	objInput             // $input
	objInputItem         // $input.item / $input.first()
	objDateTime          // $now, $today, DateTime.now()
	objJSDate            // new Date()
	objGlobal            // a namespace identifier (JSON, Math, DateTime, $execution, …)
	objNull              // null / undefined literal
)

// val is an emitted Go template operand.
type val struct {
	p      string  // Go template operand: a variable chain, literal or parenthesised call
	t      vtype   // what the emitter knows about the value's type
	numLit bool    // p is a number literal
	num    float64 // its value
	strLit bool    // p is a string literal
	str    string  // its value
	obj    objKind
	name   string // node name for node objects; namespace for objGlobal; "now"/"today" for dates
	last   bool   // node item reference asked for .last()
}

// errUnsupported marks JavaScript the translator cannot express in mono's
// template language; the caller keeps the original expression.
type errUnsupported struct{ what string }

func (e *errUnsupported) Error() string { return e.what }

func unsupported(format string, a ...interface{}) error {
	return &errUnsupported{what: fmt.Sprintf(format, a...)}
}

// emitter translates one parsed expression, collecting approximation notes.
type emitter struct {
	workflowName string
	notes        []string // approximations that keep the meaning for typical data
}

func (e *emitter) note(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	for _, n := range e.notes {
		if n == msg {
			return
		}
	}
	e.notes = append(e.notes, msg)
}

var goIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func strVal(s string) val { return val{p: strconv.Quote(s), t: tStr, strLit: true, str: s} }

func numVal(f float64) val {
	return val{p: formatNum(f), t: tNum, numLit: true, num: f}
}

func call(t vtype, fn string, args ...string) val {
	return val{p: "(" + fn + " " + strings.Join(args, " ") + ")", t: t}
}

// formatNum prints a number literal as JavaScript would show it.
func formatNum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// floatLit prints a number literal so text/template types it as float64 —
// comparisons and the arithmetic helpers need both sides to be float64.
func floatLit(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// asNum returns an operand that is a float64 at run time.
func asNum(v val) string {
	switch {
	case v.numLit:
		return floatLit(v.num)
	case v.t == tNum && !strings.HasPrefix(v.p, "(len ") && !strings.HasPrefix(v.p, "(toInt "):
		return v.p
	}
	return "(toFloat " + v.p + ")"
}

// asStr returns an operand that is a string at run time.
func asStr(v val) string {
	if v.t == tStr {
		return v.p
	}
	return "(toString " + v.p + ")"
}

// value turns n into a plain template operand.
func (e *emitter) value(n *jsNode) (val, error) {
	v, err := e.emit(n)
	if err != nil {
		return val{}, err
	}
	return e.materialize(v)
}

// materialize converts special objects that have a sensible plain value.
func (e *emitter) materialize(v val) (val, error) {
	switch v.obj {
	case objNone:
		return v, nil
	case objDateTime, objJSDate:
		return e.dateValue(v), nil
	case objNodeItem:
		return val{}, unsupported("node reference %q used without .json", v.name)
	case objNull:
		return val{}, unsupported("null/undefined used as a value")
	}
	return val{}, unsupported("%s used as a value", v.name)
}

func (e *emitter) emit(n *jsNode) (val, error) {
	switch n.kind {
	case jsNum:
		return numVal(n.num), nil
	case jsStr:
		return strVal(n.str), nil
	case jsBool:
		return val{p: strconv.FormatBool(n.b), t: tBool}, nil
	case jsNull:
		return val{obj: objNull, name: n.name}, nil
	case jsTemplate:
		return e.concat(templateOperands(n))
	case jsIdent:
		return e.ident(n.name)
	case jsMember:
		return e.member(n)
	case jsCall:
		return e.callExpr(n)
	case jsNew:
		if n.args[0].kind == jsIdent && n.args[0].name == "Date" && len(n.args) == 1 {
			return val{obj: objJSDate, name: "now"}, nil
		}
		return val{}, unsupported("'new' expressions")
	case jsUnary:
		return e.unary(n)
	case jsBinary:
		return e.binary(n)
	case jsCond:
		return val{}, unsupported("a ?: conditional nested inside another expression")
	case jsArrow:
		return val{}, unsupported("arrow functions")
	case jsRegex:
		return val{}, unsupported("regular expressions")
	case jsArray, jsObject:
		return val{}, unsupported("array/object literals")
	case jsAssign:
		return val{}, unsupported("assignments")
	case jsTypeofish:
		return val{}, unsupported("'%s'", n.name)
	}
	return val{}, unsupported("this JavaScript construct")
}

// templateOperands turns a template literal into the operands of a string
// concatenation.
func templateOperands(n *jsNode) []*jsNode {
	var ops []*jsNode
	for i, s := range n.strs {
		if s != "" {
			ops = append(ops, &jsNode{kind: jsStr, str: s})
		}
		if i < len(n.args) {
			ops = append(ops, n.args[i])
		}
	}
	if len(ops) == 0 {
		ops = append(ops, &jsNode{kind: jsStr})
	}
	return ops
}

func (e *emitter) unary(n *jsNode) (val, error) {
	switch n.name {
	case "!":
		x, err := e.value(n.args[0])
		if err != nil {
			return val{}, err
		}
		return call(tBool, "not", x.p), nil
	case "-":
		x, err := e.value(n.args[0])
		if err != nil {
			return val{}, err
		}
		if x.numLit {
			return numVal(-x.num), nil
		}
		return call(tNum, "sub", "0.0", asNum(x)), nil
	case "+":
		x, err := e.value(n.args[0])
		if err != nil {
			return val{}, err
		}
		return val{p: asNum(x), t: tNum}, nil
	}
	return val{}, unsupported("operator %q", n.name)
}

// plusOperands flattens a left-associative chain of "+".
func plusOperands(n *jsNode) []*jsNode {
	if n.kind == jsBinary && n.name == "+" {
		return append(plusOperands(n.args[0]), n.args[1])
	}
	return []*jsNode{n}
}

// isConcat decides whether a "+" chain is string concatenation (any string
// operand, or no number operand at all) or numeric addition.
func (e *emitter) isConcat(ops []val) bool {
	anyNum, anyStr := false, false
	for _, o := range ops {
		switch o.t {
		case tStr:
			anyStr = true
		case tNum:
			anyNum = true
		}
	}
	if anyStr {
		return true
	}
	if !anyNum {
		e.note("treated '+' between values of unknown type as string concatenation")
		return true
	}
	return false
}

func (e *emitter) emitAll(nodes []*jsNode) ([]val, error) {
	out := make([]val, 0, len(nodes))
	for _, n := range nodes {
		v, err := e.value(n)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (e *emitter) concat(nodes []*jsNode) (val, error) {
	ops, err := e.emitAll(nodes)
	if err != nil {
		return val{}, err
	}
	if len(ops) == 1 && ops[0].t == tStr {
		return ops[0], nil
	}
	allLit := true
	var lit strings.Builder
	for _, o := range ops {
		if !o.strLit {
			allLit = false
			break
		}
		lit.WriteString(o.str)
	}
	if allLit {
		return strVal(lit.String()), nil
	}
	args := make([]string, len(ops))
	for i, o := range ops {
		args[i] = o.p
	}
	return call(tStr, "printf", append([]string{strconv.Quote(strings.Repeat("%v", len(ops)))}, args...)...), nil
}

func (e *emitter) binary(n *jsNode) (val, error) {
	op := n.name
	switch op {
	case "+":
		nodes := plusOperands(n)
		ops, err := e.emitAll(nodes)
		if err != nil {
			return val{}, err
		}
		if e.isConcat(ops) {
			return e.concat(nodes)
		}
		acc := ops[0]
		for _, o := range ops[1:] {
			if acc.numLit && o.numLit {
				acc = numVal(acc.num + o.num)
				continue
			}
			acc = call(tNum, "add", asNum(acc), asNum(o))
		}
		return acc, nil
	case "-", "*", "/":
		l, r, err := e.pair(n)
		if err != nil {
			return val{}, err
		}
		fn := map[string]string{"-": "sub", "*": "mul", "/": "div"}[op]
		return call(tNum, fn, asNum(l), asNum(r)), nil
	case "&&", "||":
		l, r, err := e.pair(n)
		if err != nil {
			return val{}, err
		}
		t := tAny
		if l.t == r.t {
			t = l.t
		}
		return call(t, map[string]string{"&&": "and", "||": "or"}[op], l.p, r.p), nil
	case "??":
		l, r, err := e.pair(n)
		if err != nil {
			return val{}, err
		}
		return call(tAny, "default", r.p, l.p), nil
	case "==", "===", "!=", "!==":
		return e.equality(n)
	case "<", ">", "<=", ">=":
		l, r, err := e.pair(n)
		if err != nil {
			return val{}, err
		}
		fn := map[string]string{"<": "lt", ">": "gt", "<=": "le", ">=": "ge"}[op]
		if (l.t == tStr || r.t == tStr) && l.t != tNum && r.t != tNum {
			return call(tBool, fn, asStr(l), asStr(r)), nil
		}
		return call(tBool, fn, asNum(l), asNum(r)), nil
	}
	return val{}, unsupported("operator %q", op)
}

func (e *emitter) pair(n *jsNode) (val, val, error) {
	l, err := e.value(n.args[0])
	if err != nil {
		return val{}, val{}, err
	}
	r, err := e.value(n.args[1])
	if err != nil {
		return val{}, val{}, err
	}
	return l, r, nil
}

// equality emits ==, ===, != and !==. text/template's eq refuses to compare
// values of different kinds, so both sides are coerced to a common type.
func (e *emitter) equality(n *jsNode) (val, error) {
	fn := "eq"
	if n.name == "!=" || n.name == "!==" {
		fn = "ne"
	}
	l, err := e.emit(n.args[0])
	if err != nil {
		return val{}, err
	}
	r, err := e.emit(n.args[1])
	if err != nil {
		return val{}, err
	}
	if l.obj == objNull && r.obj == objNull {
		return val{p: strconv.FormatBool(fn == "eq"), t: tBool}, nil
	}
	if l.obj == objNull || r.obj == objNull {
		other := l
		if l.obj == objNull {
			other = r
		}
		if other, err = e.materialize(other); err != nil {
			return val{}, err
		}
		return call(tBool, fn, `(printf "%T" `+other.p+`)`, `"<nil>"`), nil
	}
	if l, err = e.materialize(l); err != nil {
		return val{}, err
	}
	if r, err = e.materialize(r); err != nil {
		return val{}, err
	}
	switch {
	case l.t == tNum || r.t == tNum:
		return call(tBool, fn, asNum(l), asNum(r)), nil
	case l.t == tBool && r.t == tBool:
		return call(tBool, fn, l.p, r.p), nil
	}
	return call(tBool, fn, asStr(l), asStr(r)), nil
}
