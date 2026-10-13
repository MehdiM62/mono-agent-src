package n8nimport

import (
	"fmt"
	"strings"
)

// cond is one n8n condition in a common shape: If/Filter v1 groups and
// the v2 filter type both end up here.
type cond struct {
	left, right interface{}
	typ         string // string, number, dateTime, boolean, array, object
	op          string // the v2 operation name
}

// condSet is a list of conditions joined by and/or.
type condSet struct {
	conds         []cond
	or            bool
	caseSensitive bool
}

// v1Ops maps If/Filter/Switch v1 operation names to v2 ones.
var v1Ops = map[string]string{
	"equal": "equals", "notEqual": "notEquals", "smaller": "lt", "smallerEqual": "lte",
	"larger": "gt", "largerEqual": "gte", "isEmpty": "empty", "isNotEmpty": "notEmpty",
}

func v2Op(op string) string {
	if o, ok := v1Ops[op]; ok {
		return o
	}
	return op
}

// conditionsV1 reads If/Filter v1 "conditions" ({boolean,dateTime,number,
// string}[] with value1/operation/value2).
func conditionsV1(groups map[string]interface{}, any bool) condSet {
	set := condSet{or: any, caseSensitive: true}
	defaults := map[string]string{"boolean": "equal", "dateTime": "after", "number": "smaller", "string": "equal"}
	for _, typ := range []string{"boolean", "dateTime", "number", "string"} {
		list, _ := groups[typ].([]interface{})
		for _, raw := range list {
			m, _ := raw.(map[string]interface{})
			if m == nil {
				continue
			}
			op, _ := m["operation"].(string)
			if op == "" {
				op = defaults[typ]
			}
			set.conds = append(set.conds, cond{left: m["value1"], right: m["value2"], typ: typ, op: v2Op(op)})
		}
	}
	return set
}

// conditionsV2 reads the filter type used by If v2, Filter v2 and Switch v3.
// The stored options.caseSensitive wins; when it is absent or still the
// "={{!$parameter.options.ignoreCase}}" expression, the node's ignoreCase
// option decides (n8n's default for that option is true).
func conditionsV2(f map[string]interface{}, ignoreCase bool) condSet {
	set := condSet{caseSensitive: !ignoreCase}
	if opts, ok := f["options"].(map[string]interface{}); ok {
		if cs, ok := opts["caseSensitive"].(bool); ok {
			set.caseSensitive = cs
		}
	}
	if comb, _ := f["combinator"].(string); comb == "or" {
		set.or = true
	}
	list, _ := f["conditions"].([]interface{})
	for _, raw := range list {
		m, _ := raw.(map[string]interface{})
		if m == nil {
			continue
		}
		c := cond{left: m["leftValue"], right: m["rightValue"], typ: "string", op: "equals"}
		if op, ok := m["operator"].(map[string]interface{}); ok {
			if t, _ := op["type"].(string); t != "" {
				c.typ = t
			}
			if o, _ := op["operation"].(string); o != "" {
				c.op = o
			}
		}
		set.conds = append(set.conds, c)
	}
	return set
}

// condTemplate renders a condSet as a mono template pipeline. ok is false
// when a condition cannot be expressed (the reason is in err).
func (c *MapContext) condTemplate(param string, set condSet) (string, error) {
	if len(set.conds) == 0 {
		return "true", nil
	}
	parts := make([]string, 0, len(set.conds))
	for i, cd := range set.conds {
		p, err := c.condOne(fmt.Sprintf("%s[%d]", param, i), cd, set.caseSensitive)
		if err != nil {
			return "", err
		}
		parts = append(parts, p)
	}
	if len(parts) == 1 {
		return stripParens(parts[0]), nil
	}
	fn := "and"
	if set.or {
		fn = "or"
	}
	return fn + " " + strings.Join(parts, " "), nil
}

// condOperand translates one side of a condition.
func (c *MapContext) condOperand(param string, v interface{}) (val, error) {
	tr := &exprTranslator{workflowName: c.tr.workflowName}
	out, ok := tr.operand(v)
	for _, n := range tr.notes {
		c.tr.addNote(n)
	}
	if !ok {
		reason := "cannot be expressed in mono templates"
		if len(tr.failures) > 0 {
			reason = tr.failures[0].Reason
		}
		return val{}, fmt.Errorf("%s: %s", compactValue(v), reason)
	}
	return out, nil
}

func (c *MapContext) condOne(param string, cd cond, caseSensitive bool) (string, error) {
	l, err := c.condOperand(param+".left", cd.left)
	if err != nil {
		return "", err
	}
	r := val{p: `""`, t: tStr}
	if cd.right != nil {
		if r, err = c.condOperand(param+".right", cd.right); err != nil {
			return "", err
		}
	}
	s := func(v val) string {
		if !caseSensitive && cd.typ == "string" {
			return "(lower " + asStr(v) + ")"
		}
		return asStr(v)
	}
	isNil := `(eq (printf "%T" ` + l.p + `) "<nil>")`
	switch cd.op {
	case "exists":
		return "(not " + isNil + ")", nil
	case "notExists":
		return isNil, nil
	case "empty", "notEmpty":
		e := "(not " + l.p + ")"
		switch cd.typ {
		case "string":
			e = "(eq " + asStr(l) + ` "")`
		case "number", "dateTime", "boolean":
			e = isNil
		}
		if cd.op == "notEmpty" {
			return "(not " + e + ")", nil
		}
		return e, nil
	case "true", "false":
		return `(eq (toString ` + l.p + `) "` + cd.op + `")`, nil
	}
	switch cd.typ {
	case "number":
		fn := map[string]string{"equals": "eq", "notEquals": "ne", "gt": "gt", "lt": "lt", "gte": "ge", "lte": "le"}[cd.op]
		if fn != "" {
			return "(" + fn + " " + asNum(l) + " " + asNum(r) + ")", nil
		}
	case "dateTime":
		fn := map[string]string{"equals": "eq", "notEquals": "ne", "after": "gt", "before": "lt", "afterOrEquals": "ge", "beforeOrEquals": "le"}[cd.op]
		if fn != "" {
			c.tr.addNote("date conditions compare the values as ISO 8601 text")
			return "(" + fn + " " + asStr(l) + " " + asStr(r) + ")", nil
		}
	case "boolean":
		if cd.op == "equals" || cd.op == "notEquals" {
			fn := map[string]string{"equals": "eq", "notEquals": "ne"}[cd.op]
			return "(" + fn + " (toString " + l.p + ") (toString " + r.p + "))", nil
		}
	case "array":
		return arrayCond(c, cd.op, l, r)
	default: // string, any
		switch cd.op {
		case "equals":
			return "(eq " + s(l) + " " + s(r) + ")", nil
		case "notEquals":
			return "(ne " + s(l) + " " + s(r) + ")", nil
		case "contains", "notContains":
			fn := map[string]string{"contains": "ne", "notContains": "eq"}[cd.op]
			return "(" + fn + " (replace " + s(r) + ` "" ` + s(l) + ") " + s(l) + ")", nil
		case "startsWith", "endsWith":
			return prefixCheck(s(l), s(r), cd.op == "startsWith").p, nil
		case "notStartsWith", "notEndsWith":
			return "(not " + prefixCheck(s(l), s(r), cd.op == "notStartsWith").p + ")", nil
		}
	}
	return "", fmt.Errorf("the %s condition %q has no mono template equivalent", cd.typ, cd.op)
}

func arrayCond(c *MapContext, op string, l, r val) (string, error) {
	n := "(toFloat (len " + l.p + "))"
	cmp := map[string]string{"lengthEquals": "eq", "lengthNotEquals": "ne", "lengthGt": "gt", "lengthLt": "lt", "lengthGte": "ge", "lengthLte": "le"}
	if fn, ok := cmp[op]; ok {
		return "(" + fn + " " + n + " " + asNum(r) + ")", nil
	}
	if op == "contains" || op == "notContains" {
		c.tr.addNote("list 'contains' conditions compare the JSON text of the list and the value")
		hay := "(json " + l.p + ")"
		fn := map[string]string{"contains": "ne", "notContains": "eq"}[op]
		return "(" + fn + " (replace (json " + r.p + `) "" ` + hay + ") " + hay + ")", nil
	}
	return "", fmt.Errorf("the array condition %q has no mono template equivalent", op)
}
