package n8nimport

import (
	"strings"
)

// jsCondHelpers evaluates one n8n condition in JavaScript, with the same
// operator names as the n8n filter type.
const jsCondHelpers = `function __n8nCond(l, op, t, r, cs) {
  function s(v) { v = (v === undefined || v === null) ? "" : String(v); return cs ? v : v.toLowerCase(); }
  function d(v) { return new Date(v).getTime(); }
  var isNil = l === undefined || l === null;
  switch (op) {
    case "exists": return !isNil;
    case "notExists": return isNil;
    case "empty": return isNil || l === "" || (Array.isArray(l) && l.length === 0) || (typeof l === "object" && Object.keys(l).length === 0);
    case "notEmpty": return !__n8nCond(l, "empty", t, r, cs);
    case "true": return l === true || l === "true";
    case "false": return l === false || l === "false";
    case "regex": return new RegExp(String(r), cs ? "" : "i").test(String(l));
    case "notRegex": return !new RegExp(String(r), cs ? "" : "i").test(String(l));
  }
  if (t === "number") {
    l = Number(l); r = Number(r);
    return { equals: l === r, notEquals: l !== r, gt: l > r, lt: l < r, gte: l >= r, lte: l <= r }[op];
  }
  if (t === "dateTime") {
    l = d(l); r = d(r);
    return { equals: l === r, notEquals: l !== r, after: l > r, before: l < r, afterOrEquals: l >= r, beforeOrEquals: l <= r }[op];
  }
  if (t === "array") {
    var a = Array.isArray(l) ? l : [];
    var has = a.some(function (x) { return x === r || JSON.stringify(x) === JSON.stringify(r); });
    return { contains: has, notContains: !has, lengthEquals: a.length === Number(r), lengthNotEquals: a.length !== Number(r),
      lengthGt: a.length > Number(r), lengthLt: a.length < Number(r), lengthGte: a.length >= Number(r), lengthLte: a.length <= Number(r) }[op];
  }
  if (t === "boolean") {
    return op === "equals" ? String(l) === String(r) : String(l) !== String(r);
  }
  var ls = s(l), rs = s(r);
  switch (op) {
    case "equals": return ls === rs;
    case "notEquals": return ls !== rs;
    case "contains": return ls.indexOf(rs) >= 0;
    case "notContains": return ls.indexOf(rs) < 0;
    case "startsWith": return ls.indexOf(rs) === 0;
    case "notStartsWith": return ls.indexOf(rs) !== 0;
    case "endsWith": return ls.length >= rs.length && ls.slice(ls.length - rs.length) === rs;
    case "notEndsWith": return !(ls.length >= rs.length && ls.slice(ls.length - rs.length) === rs);
  }
  throw new Error("unknown n8n condition " + t + "/" + op);
}
`

// jsCondSet renders a condSet as a JavaScript boolean expression, or ok
// false when an operand needs something core.code cannot provide.
func jsCondSet(set condSet) (string, bool) {
	if len(set.conds) == 0 {
		return "true", true
	}
	parts := make([]string, 0, len(set.conds))
	for _, cd := range set.conds {
		l, ok := jsForValue(cd.left)
		if !ok {
			return "", false
		}
		r := "undefined"
		if cd.right != nil {
			if r, ok = jsForValue(cd.right); !ok {
				return "", false
			}
		}
		parts = append(parts, "__n8nCond("+l+", "+jsQuote(cd.op)+", "+jsQuote(cd.typ)+", "+r+", "+jsValue(set.caseSensitive)+")")
	}
	join := " && "
	if set.or {
		join = " || "
	}
	return strings.Join(parts, join), true
}
