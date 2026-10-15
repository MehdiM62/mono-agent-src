package n8nimport

import (
	"fmt"
	"strings"
)

func (p *parser) arrayLiteral() (*jsNode, error) {
	p.pos++
	n := &jsNode{kind: jsArray}
	for !p.isPunct("]") {
		a, err := p.assignment()
		if err != nil {
			return nil, err
		}
		n.args = append(n.args, a)
		if p.isPunct(",") {
			p.pos++
		} else if !p.isPunct("]") {
			return nil, fmt.Errorf("expected ',' or ']' in array")
		}
	}
	p.pos++
	return n, nil
}

func (p *parser) objectLiteral() (*jsNode, error) {
	p.pos++
	n := &jsNode{kind: jsObject}
	for !p.isPunct("}") {
		t := p.peek()
		if t.kind == tkPunct && t.text == "..." {
			s, err := p.unary()
			if err != nil {
				return nil, err
			}
			n.keys = append(n.keys, "...")
			n.args = append(n.args, s)
		} else {
			if t.kind != tkIdent && t.kind != tkString && t.kind != tkNumber {
				return nil, fmt.Errorf("unsupported object key %q", t.text)
			}
			p.pos++
			key := t.text
			var val *jsNode
			if p.isPunct(":") {
				p.pos++
				v, err := p.assignment()
				if err != nil {
					return nil, err
				}
				val = v
			} else {
				val = &jsNode{kind: jsIdent, name: key} // shorthand {a}
			}
			n.keys = append(n.keys, key)
			n.args = append(n.args, val)
		}
		if p.isPunct(",") {
			p.pos++
		} else if !p.isPunct("}") {
			return nil, fmt.Errorf("expected ',' or '}' in object")
		}
	}
	p.pos++
	return n, nil
}

// parseTemplateLiteral splits a backtick body into its text and ${} parts.
func parseTemplateLiteral(body string) (*jsNode, error) {
	n := &jsNode{kind: jsTemplate}
	var text strings.Builder
	for i := 0; i < len(body); {
		c := body[i]
		if c == '\\' && i+1 < len(body) {
			text.WriteString(unescapeJS(body[i+1]))
			i += 2
			continue
		}
		if strings.HasPrefix(body[i:], "${") {
			end, err := findBraceEnd(body[i+2:])
			if err != nil {
				return nil, err
			}
			inner := body[i+2 : i+2+end]
			x, err := parseJS(inner)
			if err != nil {
				return nil, err
			}
			n.strs = append(n.strs, text.String())
			text.Reset()
			n.args = append(n.args, x)
			i += 2 + end + 1
			continue
		}
		text.WriteByte(c)
		i++
	}
	n.strs = append(n.strs, text.String())
	return n, nil
}
