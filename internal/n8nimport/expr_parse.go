package n8nimport

import "fmt"

// jsKind is the kind of a node in the small JavaScript AST the translator
// works on.
type jsKind int

const (
	jsIdent jsKind = iota
	jsNum
	jsStr
	jsBool
	jsNull      // null or undefined
	jsTemplate  // `…${x}…`: strs[i] text, args[i] expressions (len(strs) == len(args)+1)
	jsRegex     // str = body/flags
	jsMember    // args[0].name, or args[0][args[1]] when computed
	jsCall      // args[0](args[1:]…)
	jsUnary     // op args[0]
	jsBinary    // args[0] op args[1] (arithmetic, comparison, logical)
	jsCond      // args[0] ? args[1] : args[2]
	jsArray     // [args…]
	jsObject    // {keys[i]: args[i]}
	jsArrow     // arrow function, never translated
	jsNew       // new args[0](args[1:]…)
	jsSeq       // a, b
	jsAssign    // assignments, never translated
	jsSpread    // ...x
	jsTypeofish // typeof / void / delete
)

type jsNode struct {
	kind     jsKind
	name     string // identifier, member name, operator
	str      string
	num      float64
	b        bool
	computed bool
	optional bool
	args     []*jsNode
	strs     []string
	keys     []string
}

type parser struct {
	toks []token
	pos  int
}

// parseJS parses one JavaScript expression (n8n {{ }} body).
func parseJS(src string) (*jsNode, error) {
	toks, err := lexJS(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	n, err := p.expression()
	if err != nil {
		return nil, err
	}
	// A trailing semicolon is harmless.
	for p.isPunct(";") {
		p.pos++
	}
	if p.peek().kind != tkEOF {
		return nil, fmt.Errorf("unexpected %q", p.peek().text)
	}
	return n, nil
}

func (p *parser) peek() token { return p.toks[p.pos] }

func (p *parser) isPunct(s string) bool {
	t := p.toks[p.pos]
	return t.kind == tkPunct && t.text == s
}

func (p *parser) expect(s string) error {
	if !p.isPunct(s) {
		return fmt.Errorf("expected %q, got %q", s, p.peek().text)
	}
	p.pos++
	return nil
}

func (p *parser) expression() (*jsNode, error) {
	n, err := p.assignment()
	if err != nil {
		return nil, err
	}
	if p.isPunct(",") {
		seq := &jsNode{kind: jsSeq, args: []*jsNode{n}}
		for p.isPunct(",") {
			p.pos++
			m, err := p.assignment()
			if err != nil {
				return nil, err
			}
			seq.args = append(seq.args, m)
		}
		return seq, nil
	}
	return n, nil
}

var assignOps = map[string]bool{"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true,
	"&&=": true, "||=": true, "??=": true, "**=": true, "<<=": true, ">>=": true, ">>>=": true, "&=": true, "|=": true, "^=": true}

func (p *parser) assignment() (*jsNode, error) {
	if arrow, ok, err := p.tryArrow(); ok || err != nil {
		return arrow, err
	}
	n, err := p.conditional()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind == tkPunct && assignOps[t.text] {
		p.pos++
		rhs, err := p.assignment()
		if err != nil {
			return nil, err
		}
		return &jsNode{kind: jsAssign, name: t.text, args: []*jsNode{n, rhs}}, nil
	}
	return n, nil
}

// tryArrow recognises `x => …` and `(a, b) => …`. The body is parsed (so
// the segment end is right) but the node is never translated.
func (p *parser) tryArrow() (*jsNode, bool, error) {
	start := p.pos
	t := p.peek()
	if t.kind == tkIdent && p.toks[p.pos+1].kind == tkPunct && p.toks[p.pos+1].text == "=>" {
		p.pos += 2
		return p.arrowBody()
	}
	if t.kind == tkIdent && t.text == "async" {
		p.pos++
		if n, ok, err := p.tryArrow(); ok || err != nil {
			return n, ok, err
		}
		p.pos = start
		return nil, false, nil
	}
	if !p.isPunct("(") {
		return nil, false, nil
	}
	depth := 0
	for i := p.pos; i < len(p.toks); i++ {
		tk := p.toks[i]
		if tk.kind == tkEOF {
			break
		}
		if tk.kind == tkPunct && (tk.text == "(" || tk.text == "[" || tk.text == "{") {
			depth++
		}
		if tk.kind == tkPunct && (tk.text == ")" || tk.text == "]" || tk.text == "}") {
			depth--
			if depth == 0 {
				if n := p.toks[i+1]; n.kind == tkPunct && n.text == "=>" {
					p.pos = i + 2
					return p.arrowBody()
				}
				break
			}
		}
	}
	p.pos = start
	return nil, false, nil
}

func (p *parser) arrowBody() (*jsNode, bool, error) {
	if p.isPunct("{") {
		if err := p.skipBalanced("{", "}"); err != nil {
			return nil, true, err
		}
		return &jsNode{kind: jsArrow}, true, nil
	}
	if _, err := p.assignment(); err != nil {
		return nil, true, err
	}
	return &jsNode{kind: jsArrow}, true, nil
}

func (p *parser) skipBalanced(open, close string) error {
	depth := 0
	for p.peek().kind != tkEOF {
		t := p.peek()
		p.pos++
		if t.kind == tkPunct && t.text == open {
			depth++
		} else if t.kind == tkPunct && t.text == close {
			depth--
			if depth == 0 {
				return nil
			}
		}
	}
	return fmt.Errorf("unbalanced %s", open)
}

func (p *parser) conditional() (*jsNode, error) {
	test, err := p.binary(0)
	if err != nil {
		return nil, err
	}
	if !p.isPunct("?") {
		return test, nil
	}
	p.pos++
	cons, err := p.assignment()
	if err != nil {
		return nil, err
	}
	if err := p.expect(":"); err != nil {
		return nil, err
	}
	alt, err := p.assignment()
	if err != nil {
		return nil, err
	}
	return &jsNode{kind: jsCond, args: []*jsNode{test, cons, alt}}, nil
}

// binary operator precedence (higher binds tighter).
var binPrec = map[string]int{
	"??": 1, "||": 2, "&&": 3, "|": 4, "^": 5, "&": 6,
	"==": 7, "!=": 7, "===": 7, "!==": 7,
	"<": 8, ">": 8, "<=": 8, ">=": 8, "instanceof": 8, "in": 8,
	"<<": 9, ">>": 9, ">>>": 9,
	"+": 10, "-": 10, "*": 11, "/": 11, "%": 11, "**": 12,
}

func (p *parser) binary(minPrec int) (*jsNode, error) {
	left, err := p.unary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		op := t.text
		if t.kind != tkPunct && !(t.kind == tkIdent && (op == "instanceof" || op == "in")) {
			return left, nil
		}
		prec, ok := binPrec[op]
		if !ok || prec <= minPrec {
			return left, nil
		}
		p.pos++
		next := prec
		if op == "**" {
			next = prec - 1 // right associative
		}
		right, err := p.binary(next)
		if err != nil {
			return nil, err
		}
		left = &jsNode{kind: jsBinary, name: op, args: []*jsNode{left, right}}
	}
}

func (p *parser) unary() (*jsNode, error) {
	t := p.peek()
	if t.kind == tkPunct && (t.text == "!" || t.text == "-" || t.text == "+" || t.text == "~" || t.text == "++" || t.text == "--") {
		p.pos++
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &jsNode{kind: jsUnary, name: t.text, args: []*jsNode{x}}, nil
	}
	if t.kind == tkIdent && (t.text == "typeof" || t.text == "void" || t.text == "delete" || t.text == "await") {
		p.pos++
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &jsNode{kind: jsTypeofish, name: t.text, args: []*jsNode{x}}, nil
	}
	if t.kind == tkPunct && t.text == "..." {
		p.pos++
		x, err := p.assignment()
		if err != nil {
			return nil, err
		}
		return &jsNode{kind: jsSpread, args: []*jsNode{x}}, nil
	}
	n, err := p.postfix()
	if err != nil {
		return nil, err
	}
	if p.isPunct("++") || p.isPunct("--") {
		p.pos++
		return &jsNode{kind: jsAssign, name: "++", args: []*jsNode{n}}, nil
	}
	return n, nil
}

func (p *parser) postfix() (*jsNode, error) {
	var n *jsNode
	var err error
	if p.peek().kind == tkIdent && p.peek().text == "new" {
		p.pos++
		callee, err := p.primary()
		if err != nil {
			return nil, err
		}
		for p.isPunct(".") {
			p.pos++
			callee = &jsNode{kind: jsMember, name: p.peek().text, args: []*jsNode{callee}}
			p.pos++
		}
		n = &jsNode{kind: jsNew, args: []*jsNode{callee}}
		if p.isPunct("(") {
			args, err := p.arguments()
			if err != nil {
				return nil, err
			}
			n.args = append(n.args, args...)
		}
	} else if n, err = p.primary(); err != nil {
		return nil, err
	}
	for {
		switch {
		case p.isPunct(".") || p.isPunct("?."):
			optional := p.isPunct("?.")
			p.pos++
			if optional && p.isPunct("(") {
				args, err := p.arguments()
				if err != nil {
					return nil, err
				}
				n = &jsNode{kind: jsCall, args: append([]*jsNode{n}, args...)}
				continue
			}
			if optional && p.isPunct("[") {
				continue // x?.[k] is handled by the "[" case
			}
			t := p.peek()
			if t.kind != tkIdent {
				return nil, fmt.Errorf("expected property name after '.', got %q", t.text)
			}
			p.pos++
			n = &jsNode{kind: jsMember, name: t.text, optional: optional, args: []*jsNode{n}}
		case p.isPunct("["):
			p.pos++
			idx, err := p.expression()
			if err != nil {
				return nil, err
			}
			if err := p.expect("]"); err != nil {
				return nil, err
			}
			n = &jsNode{kind: jsMember, computed: true, args: []*jsNode{n, idx}}
		case p.isPunct("("):
			args, err := p.arguments()
			if err != nil {
				return nil, err
			}
			n = &jsNode{kind: jsCall, args: append([]*jsNode{n}, args...)}
		case p.peek().kind == tkTemplate:
			return nil, fmt.Errorf("tagged template literals are not supported")
		default:
			return n, nil
		}
	}
}

func (p *parser) arguments() ([]*jsNode, error) {
	if err := p.expect("("); err != nil {
		return nil, err
	}
	var args []*jsNode
	for !p.isPunct(")") {
		a, err := p.assignment()
		if err != nil {
			return nil, err
		}
		args = append(args, a)
		if p.isPunct(",") {
			p.pos++
			continue
		}
		if !p.isPunct(")") {
			return nil, fmt.Errorf("expected ',' or ')' in call, got %q", p.peek().text)
		}
	}
	p.pos++
	return args, nil
}

func (p *parser) primary() (*jsNode, error) {
	t := p.peek()
	switch t.kind {
	case tkNumber:
		p.pos++
		return &jsNode{kind: jsNum, num: t.num, str: t.text}, nil
	case tkString:
		p.pos++
		return &jsNode{kind: jsStr, str: t.text}, nil
	case tkRegex:
		p.pos++
		return &jsNode{kind: jsRegex, str: t.text}, nil
	case tkTemplate:
		p.pos++
		return parseTemplateLiteral(t.text)
	case tkIdent:
		p.pos++
		switch t.text {
		case "true", "false":
			return &jsNode{kind: jsBool, b: t.text == "true"}, nil
		case "null", "undefined":
			return &jsNode{kind: jsNull, name: t.text}, nil
		case "function":
			return nil, fmt.Errorf("function expressions are not supported")
		}
		return &jsNode{kind: jsIdent, name: t.text}, nil
	case tkPunct:
		switch t.text {
		case "(":
			p.pos++
			n, err := p.expression()
			if err != nil {
				return nil, err
			}
			return n, p.expect(")")
		case "[":
			return p.arrayLiteral()
		case "{":
			return p.objectLiteral()
		}
	}
	if t.kind == tkEOF {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	return nil, fmt.Errorf("unexpected %q", t.text)
}
