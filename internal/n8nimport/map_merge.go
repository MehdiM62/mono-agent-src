package n8nimport

import (
	"fmt"
	"strings"
)

func init() {
	Register("n8n-nodes-base.merge", mapMerge)
	Register("n8n-nodes-base.compareDatasets", mapCompareDatasets)
}

// mergeSpec drives the generated JavaScript for Merge modes beyond append.
type mergeSpec struct {
	Op       string      `json:"op"` // choose, position, fields, cross
	Input    int         `json:"input,omitempty"`
	Join     string      `json:"join,omitempty"`   // keepMatches, keepNonMatches, keepEverything, enrichInput1, enrichInput2
	Output   string      `json:"output,omitempty"` // both, input1, input2
	Fields   [][2]string `json:"fields,omitempty"`
	Clash    string      `json:"clash,omitempty"` // preferInput1, preferInput2 (preferLast), addSuffix
	Multiple string      `json:"multiple,omitempty"`
	Unpaired bool        `json:"unpaired,omitempty"`
	Fill     string      `json:"fill,omitempty"` // mergeByKey overwrite: always, blank, undefined
}

// jsMergeHelpers groups tagged items by input and combines them.
const jsMergeHelpers = `function __n8nGet(o, p) { return String(p).split(".").reduce(function (a, k) { return a === undefined || a === null ? undefined : a[k]; }, o); }
function __n8nInputs() {
  var ins = [];
  items.forEach(function (it) { var j = Object.assign({}, it.json); var k = Number(j.__n8n_input) || 0; delete j.__n8n_input; (ins[k] = ins[k] || []).push(j); });
  for (var i = 0; i < ins.length; i++) { ins[i] = ins[i] || []; }
  return ins;
}
function __n8nCombine(a, b, s) {
  var o = {};
  var clash = s.clash || "preferInput2";
  if (clash === "addSuffix") {
    Object.keys(a).forEach(function (k) { o[k in b ? k + "_1" : k] = a[k]; });
    Object.keys(b).forEach(function (k) { o[k in a ? k + "_2" : k] = b[k]; });
    return o;
  }
  if (s.fill) {
    Object.assign(o, a);
    Object.keys(b).forEach(function (k) {
      if (s.fill === "always" || (s.fill === "undefined" && o[k] === undefined) || (s.fill === "blank" && (o[k] === undefined || o[k] === null || o[k] === ""))) { o[k] = b[k]; }
    });
    return o;
  }
  return clash === "preferInput1" ? Object.assign(o, b, a) : Object.assign(o, a, b);
}
function __n8nMerge(s) {
  var ins = __n8nInputs();
  var a = ins[0] || [], b = ins[1] || [];
  if (s.op === "choose") { return ins[s.input] || []; }
  if (s.op === "position") {
    var n = s.unpaired ? Math.max(a.length, b.length) : Math.min(a.length, b.length);
    var out = [];
    for (var i = 0; i < n; i++) { out.push(__n8nCombine(a[i] || {}, b[i] || {}, s)); }
    return out;
  }
  if (s.op === "cross") {
    var x = [];
    a.forEach(function (l) { b.forEach(function (r) { x.push(__n8nCombine(l, r, s)); }); });
    return x;
  }
  function key(o, side) { return JSON.stringify((s.fields || []).map(function (f) { var v = __n8nGet(o, f[side]); return v === undefined ? null : v; })); }
  var index = {};
  b.forEach(function (r, i) { var k = key(r, 1); (index[k] = index[k] || []).push(i); });
  var usedB = {}, res = [], onlyA = [];
  a.forEach(function (l) {
    var hits = index[key(l, 0)] || [];
    if (s.multiple === "first") { hits = hits.slice(0, 1); }
    if (!hits.length) { onlyA.push(l); if (s.join === "keepEverything" || s.join === "enrichInput1") { res.push(l); } return; }
    hits.forEach(function (i) {
      usedB[i] = true;
      if (s.join === "keepNonMatches") { return; }
      if (s.join === "keepMatches" && s.output === "input1") { res.push(l); return; }
      if (s.join === "keepMatches" && s.output === "input2") { res.push(b[i]); return; }
      res.push(__n8nCombine(l, b[i], s));
    });
  });
  var onlyB = b.filter(function (_, i) { return !usedB[i]; });
  if (s.join === "keepNonMatches") {
    if (s.output === "input1") { return onlyA; }
    if (s.output === "input2") { return onlyB; }
    return onlyA.concat(onlyB);
  }
  if (s.join === "keepEverything" || s.join === "enrichInput2") { res = res.concat(onlyB); }
  if (s.join === "keepMatches" && s.output === "input1") {
    var seen = []; res = res.filter(function (r) { if (seen.indexOf(r) >= 0) { return false; } seen.push(r); return true; });
  }
  return res;
}
`

// mergeCode is the Mapping for a merge mode run in JavaScript: inputs are
// tagged, collected by core.merge, then combined by core.code.
func mergeCode(spec mergeSpec) Mapping {
	body := jsMergeHelpers + "return __n8nMerge(" + jsValue(spec) + ");\n"
	return Mapping{Type: "core.merge", Config: map[string]interface{}{}, TagInputs: true, EntrySuffix: " (collect inputs)",
		Chain: []ChainStep{{Type: "core.code", Config: codeConfig(wrapCode(body))}}}
}

// matchFields reads "field1/field2" pairs.
func matchFields(list []map[string]interface{}) [][2]string {
	var out [][2]string
	for _, v := range list {
		out = append(out, [2]string{fmt.Sprint(v["field1"]), fmt.Sprint(v["field2"])})
	}
	return out
}

func mapMerge(c *MapContext) Mapping {
	c.Ignore("numberInputs", "options.disableDotNotation", "options.fuzzyCompare")
	if c.Bool("options.fuzzyCompare", false) {
		c.Note("fuzzy compare is not reproduced: values match only when they are equal")
	}
	mode := c.Literal("mode", "append")
	switch {
	case !c.version(2):
		return mergeV1(c, mode)
	}
	clash := c.Literal("options.clashHandling.values.resolveClash", "")
	if clash == "preferLast" {
		clash = "preferInput2"
	}
	c.Ignore("options.clashHandling")
	switch mode {
	case "append":
		return Mapping{Type: "core.merge", Config: map[string]interface{}{}}
	case "chooseBranch":
		c.Ignore("chooseBranchMode")
		def := "input1"
		if c.version(3) {
			def = "specifiedInput"
		}
		out := c.Literal("output", def)
		input := 0
		switch {
		case out == "empty":
			return mergeCode(mergeSpec{Op: "choose", Input: 99})
		case out == "input2":
			input = 1
		case out == "specifiedInput":
			input = int(c.Float("useDataOfInput", 1)) - 1
		}
		return mergeCode(mergeSpec{Op: "choose", Input: input})
	case "combineBySql":
		c.Warn("query", c.Raw("query"), "SQL merges have no mono-agent equivalent; the inputs are appended")
		return Mapping{Type: "core.merge", Config: map[string]interface{}{}}
	case "combine", "mergeByIndex", "mergeByKey", "multiplex":
	default:
		return c.OperationUnsupported("", mode)
	}
	by := c.Literal("combineBy", "combineByFields")
	if !c.version(3) {
		by = map[string]string{"mergeByFields": "combineByFields", "mergeByPosition": "combineByPosition", "multiplex": "combineAll"}[c.Literal("combinationMode", "mergeByFields")]
	}
	spec := mergeSpec{Clash: clash, Multiple: c.Literal("options.multipleMatches", "all")}
	switch by {
	case "combineByPosition":
		spec.Op = "position"
		spec.Unpaired = c.Bool("options.includeUnpaired", false)
		if spec.Clash == "" && c.version(3) {
			spec.Clash = "addSuffix"
		}
	case "combineAll":
		spec.Op = "cross"
	default:
		spec.Op = "fields"
		spec.Join = c.Literal("joinMode", "keepMatches")
		spec.Output = c.Literal("outputDataFrom", "both")
		if c.version(3) && !c.Bool("advanced", false) {
			for _, f := range strings.Split(c.Literal("fieldsToMatchString", ""), ",") {
				if f = strings.TrimSpace(f); f != "" {
					spec.Fields = append(spec.Fields, [2]string{f, f})
				}
			}
		} else {
			spec.Fields = matchFields(c.paramList("mergeByFields.values"))
		}
	}
	return mergeCode(spec)
}

func mergeV1(c *MapContext, mode string) Mapping {
	keys := [][2]string{{c.Literal("propertyName1", ""), c.Literal("propertyName2", "")}}
	switch mode {
	case "append":
		return Mapping{Type: "core.merge", Config: map[string]interface{}{}}
	case "passThrough":
		input := 0
		if c.Literal("output", "input1") == "input2" {
			input = 1
		}
		return mergeCode(mergeSpec{Op: "choose", Input: input})
	case "wait":
		return mergeCode(mergeSpec{Op: "choose", Input: 99})
	case "mergeByIndex":
		join := c.Literal("join", "left")
		spec := mergeSpec{Op: "position", Unpaired: join == "outer"}
		if join == "left" {
			c.Note("left join by index keeps the shorter length when input 2 has fewer items")
		}
		return mergeCode(spec)
	case "multiplex":
		return mergeCode(mergeSpec{Op: "cross"})
	case "mergeByKey":
		return mergeCode(mergeSpec{Op: "fields", Join: "enrichInput1", Fields: keys, Fill: c.Literal("overwrite", "always"), Multiple: "first"})
	case "keepKeyMatches":
		return mergeCode(mergeSpec{Op: "fields", Join: "keepMatches", Output: "input1", Fields: keys})
	case "removeKeyMatches":
		return mergeCode(mergeSpec{Op: "fields", Join: "keepNonMatches", Output: "input1", Fields: keys})
	}
	return c.OperationUnsupported("", mode)
}

// jsCompareHelpers splits two inputs into n8n's four Compare Datasets
// outputs and records the output index in __n8n_output.
const jsCompareHelpers = `function __n8nCompare(s) {
  var ins = __n8nInputs();
  var a = ins[0] || [], b = ins[1] || [];
  var skip = s.skip || [];
  function key(o, side) { return JSON.stringify((s.fields || []).map(function (f) { var v = __n8nGet(o, f[side]); return v === undefined ? null : v; })); }
  function strip(o) { var c = Object.assign({}, o); skip.forEach(function (k) { delete c[k]; }); return c; }
  function same(x, y) { return JSON.stringify(strip(x), Object.keys(strip(x)).sort()) === JSON.stringify(strip(y), Object.keys(strip(y)).sort()); }
  var index = {};
  b.forEach(function (r, i) { var k = key(r, 1); if (!(k in index)) { index[k] = i; } });
  var out = [], usedB = {};
  a.forEach(function (l) {
    var k = key(l, 0);
    if (!(k in index)) { out.push(Object.assign({}, l, { __n8n_output: "0" })); return; }
    var r = b[index[k]]; usedB[index[k]] = true;
    if (same(l, r)) { out.push(Object.assign({}, l, { __n8n_output: "1" })); return; }
    var v;
    if (s.resolve === "preferInput1") { v = Object.assign({}, l); }
    else if (s.resolve === "preferInput2") { v = Object.assign({}, r); }
    else if (s.resolve === "mix") { v = Object.assign({}, s.prefer === "input1" ? r : l, s.prefer === "input1" ? l : r); }
    else {
      var diff = {}, eq = {};
      Object.keys(Object.assign({}, l, r)).forEach(function (f) {
        if (JSON.stringify(l[f]) === JSON.stringify(r[f])) { eq[f] = l[f]; } else { diff[f] = { inputA: l[f], inputB: r[f] }; }
      });
      v = { keys: JSON.parse(key(l, 0)), same: eq, different: diff };
    }
    v.__n8n_output = "2"; out.push(v);
  });
  b.forEach(function (r, i) { if (!usedB[i]) { out.push(Object.assign({}, r, { __n8n_output: "3" })); } });
  return out;
}
`

func mapCompareDatasets(c *MapContext) Mapping {
	c.Ignore("options.disableDotNotation", "options.multipleMatches", "exceptWhenMix")
	resolve := c.Literal("resolve", "preferInput2")
	if c.version(2.1) {
		resolve = c.Literal("resolve", "includeBoth")
	}
	var skip []string
	for _, f := range strings.Split(c.Literal("options.skipFields", ""), ",") {
		if f = strings.TrimSpace(f); f != "" {
			skip = append(skip, f)
		}
	}
	spec := map[string]interface{}{
		"fields": matchFields(c.paramList("mergeByFields.values")), "resolve": resolve,
		"prefer": c.Literal("preferWhenMix", "input1"), "skip": skip,
	}
	if c.Bool("fuzzyCompare", false) || c.Bool("options.fuzzyCompare", false) {
		c.Note("fuzzy compare is not reproduced: values match only when they are equal")
	}
	body := jsMergeHelpers + jsCompareHelpers + "return __n8nCompare(" + jsValue(spec) + ");\n"
	c.Warn("outputs", nil, "items carry the helper field __n8n_output that routed them")
	return Mapping{Type: "core.merge", Config: map[string]interface{}{}, TagInputs: true, EntrySuffix: " (collect inputs)",
		Outputs: switchHandles(4),
		Chain: []ChainStep{
			{Type: "core.code", Suffix: " (compare)", Config: codeConfig(wrapCode(body))},
			{Type: "core.switch", Config: map[string]interface{}{"field": "{{ $json.__n8n_output }}",
				"cases": []interface{}{"0", "1", "2", "3"}, "default_handle": "none"}},
		}}
}
