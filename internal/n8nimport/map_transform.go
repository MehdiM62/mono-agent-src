package n8nimport

import (
	"fmt"
	"strings"
)

func init() {
	Register("n8n-nodes-base.aggregate", mapAggregate)
	Register("n8n-nodes-base.sort", mapSort)
	Register("n8n-nodes-base.limit", mapLimit)
	Register("n8n-nodes-base.removeDuplicates", mapRemoveDuplicates)
	Register("n8n-nodes-base.splitOut", mapSplitOut)
	Register("n8n-nodes-base.renameKeys", mapRenameKeys)
	Register("n8n-nodes-base.summarize", mapSummarize)
	Register("n8n-nodes-base.itemLists", mapItemLists)
}

// commaList splits an n8n comma-separated field list.
func commaList(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// jsCode is a core.code Mapping running body over all items with the
// shared helpers available.
func jsCode(body string) Mapping {
	return Mapping{Type: "core.code", Config: codeConfig(wrapCode(jsMergeHelpers + jsValueHelpers + body))}
}

func mapAggregate(c *MapContext) Mapping {
	c.Ignore("options.disableDotNotation", "options.includeBinaries")
	if c.Literal("aggregate", "aggregateIndividualFields") == "aggregateAllItemData" {
		dest := c.Literal("destinationFieldName", "data")
		include := c.Literal("include", "allFields")
		inc := commaList(c.Literal("fieldsToInclude", ""))
		exc := commaList(c.Literal("fieldsToExclude", ""))
		mode := map[string]string{"allFields": "all", "specifiedFields": "selected", "allFieldsExcept": "except"}[include]
		fields := inc
		if mode == "except" {
			fields = exc
		}
		return jsCode(fmt.Sprintf("var o = {};\no[%s] = items.map(function (it) { return %s; });\nreturn [o];\n",
			jsQuote(dest), jsBase(mode, fields, "it.json")))
	}
	type agg struct {
		Field  string `json:"field"`
		Output string `json:"output"`
	}
	list := []agg{}
	for _, f := range c.paramList("fieldsToAggregate.fieldToAggregate") {
		field := fmt.Sprint(f["fieldToAggregate"])
		out := field
		if r, _ := f["renameField"].(bool); r {
			out = fmt.Sprint(f["outputFieldName"])
		}
		list = append(list, agg{Field: field, Output: out})
	}
	opts := map[string]bool{
		"merge":   c.Bool("options.mergeLists", false),
		"unique":  c.Bool("options.keepOnlyUnique", false),
		"missing": c.Bool("options.keepMissing", false),
	}
	body := fmt.Sprintf(`var spec = %s, opt = %s, o = {};
spec.forEach(function (f) {
  var vals = [];
  items.forEach(function (it) {
    var v = __n8nGet(it.json, f.field);
    if (v === undefined || (v === null && !opt.missing)) { if (opt.missing) { vals.push(null); } return; }
    if (opt.merge && Array.isArray(v)) { vals = vals.concat(v); } else { vals.push(v); }
  });
  if (opt.unique) { var seen = {}; vals = vals.filter(function (v) { var k = JSON.stringify(v); if (seen[k]) { return false; } seen[k] = true; return true; }); }
  o[f.output] = vals;
});
return [o];
`, jsValue(list), jsValue(opts))
	return jsCode(body)
}

func mapSort(c *MapContext) Mapping {
	c.Ignore("options.disableDotNotation")
	switch c.Literal("type", "simple") {
	case "random":
		return jsCode("var a = items.map(function (it) { return it.json; });\nfor (var i = a.length - 1; i > 0; i--) { var j = Math.floor(Math.random() * (i + 1)); var t = a[i]; a[i] = a[j]; a[j] = t; }\nreturn a;\n")
	case "code":
		code := importCode(c, "code", fmt.Sprint(c.Raw("code")))
		return jsCode("var __cmp = function (a, b) {\n" + indentJS(code) + "};\nreturn items.map(function (it) { return it.json; }).sort(__cmp);\n")
	}
	type key struct {
		Field string `json:"field"`
		Desc  bool   `json:"desc"`
	}
	keys := []key{}
	for _, f := range c.paramList("sortFieldsUi.sortField") {
		keys = append(keys, key{Field: fmt.Sprint(f["fieldName"]), Desc: f["order"] == "descending"})
	}
	return jsCode(fmt.Sprintf(`var keys = %s;
function cmp(x, y) {
  if (x === y) { return 0; }
  if (x === undefined || x === null) { return 1; }
  if (y === undefined || y === null) { return -1; }
  if (typeof x === "number" && typeof y === "number") { return x - y; }
  if (typeof x === "object" || typeof y === "object") { x = JSON.stringify(x); y = JSON.stringify(y); }
  return String(x) < String(y) ? -1 : String(x) > String(y) ? 1 : 0;
}
return items.map(function (it) { return it.json; }).sort(function (a, b) {
  for (var i = 0; i < keys.length; i++) {
    var r = cmp(__n8nGet(a, keys[i].field), __n8nGet(b, keys[i].field));
    if (r !== 0) { return keys[i].desc ? -r : r; }
  }
  return 0;
});
`, jsValue(keys)))
}

func mapLimit(c *MapContext) Mapping {
	max := c.Float("maxItems", 1)
	if c.Literal("keep", "firstItems") == "lastItems" {
		return jsCode(fmt.Sprintf("var n = %s;\nreturn items.slice(Math.max(0, items.length - n)).map(function (it) { return it.json; });\n", jsValue(max)))
	}
	return Mapping{Type: "core.limit", Config: map[string]interface{}{"max_items": max}}
}

func mapRemoveDuplicates(c *MapContext) Mapping {
	if c.version(2) {
		if op := c.Literal("operation", "removeDuplicateInputItems"); op != "removeDuplicateInputItems" {
			c.Ignore("logic", "dedupeValue", "incrementalDedupeValue", "dateDedupeValue", "mode", "options.scope", "options.historySize")
			return c.OperationUnsupported("", op)
		}
	}
	c.Ignore("options.disableDotNotation")
	compare := c.Literal("compare", "allFields")
	removeOther := c.Bool("options.removeOtherFields", false)
	var fields []string
	switch compare {
	case "allFields":
		if !removeOther {
			return Mapping{Type: "core.remove_duplicates", Config: map[string]interface{}{"keep": "first"}}
		}
	case "selectedFields":
		fields = commaList(c.Literal("fieldsToCompare", ""))
		if len(fields) == 1 && !removeOther {
			return Mapping{Type: "core.remove_duplicates", Config: map[string]interface{}{"field": fields[0], "keep": "first"}}
		}
	case "allFieldsExcept":
		fields = commaList(c.Literal("fieldsToExclude", ""))
	}
	return jsCode(fmt.Sprintf(`var mode = %s, fields = %s, removeOther = %s, seen = {}, out = [];
items.forEach(function (it) {
  var j = it.json, k;
  if (mode === "selectedFields") { k = fields.map(function (f) { return __n8nGet(j, f); }); }
  else { var c = Object.assign({}, j); if (mode === "allFieldsExcept") { fields.forEach(function (f) { delete c[f]; }); } k = Object.keys(c).sort().map(function (x) { return [x, c[x]]; }); }
  k = JSON.stringify(k);
  if (seen[k]) { return; }
  seen[k] = true;
  if (removeOther && mode === "selectedFields") { var o = {}; fields.forEach(function (f) { __n8nSet(o, f, __n8nGet(j, f)); }); out.push(o); } else { out.push(j); }
});
return out;
`, jsQuote(compare), jsValue(fields), jsValue(removeOther)))
}

func mapSplitOut(c *MapContext) Mapping {
	c.Ignore("options.disableDotNotation", "options.includeBinary")
	fields := commaList(c.Literal("fieldToSplitOut", ""))
	include := c.Literal("include", "noOtherFields")
	keep := commaList(c.Literal("fieldsToInclude", ""))
	dest := c.Literal("options.destinationFieldName", "")
	return jsCode(fmt.Sprintf(`var fields = %s, include = %s, keep = %s, dest = %s, out = [];
items.forEach(function (it) {
  var j = it.json;
  fields.forEach(function (f) {
    var v = __n8nGet(j, f);
    var list = Array.isArray(v) ? v : (v && typeof v === "object" ? Object.values(v) : (v === undefined ? [] : [v]));
    list.forEach(function (x) {
      var o = {};
      if (include === "allOtherFields") { o = Object.assign({}, j); delete o[f]; }
      if (include === "selectedOtherFields") { keep.forEach(function (k) { o[k] = __n8nGet(j, k); }); }
      if (x !== null && typeof x === "object" && !Array.isArray(x) && !dest) { Object.assign(o, x); } else { o[dest || f] = x; }
      out.push(o);
    });
  });
});
return out;
`, jsValue(fields), jsQuote(include), jsValue(keep), jsQuote(dest)))
}

func mapRenameKeys(c *MapContext) Mapping {
	type pair struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	pairs := []pair{}
	for _, k := range c.paramList("keys.key") {
		pairs = append(pairs, pair{From: fmt.Sprint(k["currentKey"]), To: fmt.Sprint(k["newKey"])})
	}
	type rx struct {
		Search  string `json:"search"`
		Replace string `json:"replace"`
		CI      bool   `json:"ci"`
	}
	rxs := []rx{}
	for _, r := range c.paramList("additionalOptions.regexReplacement.replacements") {
		opts, _ := r["options"].(map[string]interface{})
		ci, _ := opts["caseInsensitive"].(bool)
		if d, ok := opts["depth"].(float64); ok && d != -1 && d != 0 {
			c.Note("regex key renaming applies to top-level keys only")
		}
		rxs = append(rxs, rx{Search: fmt.Sprint(r["searchRegex"]), Replace: fmt.Sprint(r["replaceRegex"]), CI: ci})
	}
	c.Ignore("additionalOptions.regexReplacement")
	return jsCode(fmt.Sprintf(`var pairs = %s, rxs = %s;
return items.map(function (it) {
  var o = JSON.parse(JSON.stringify(it.json));
  pairs.forEach(function (p) { var v = __n8nGet(o, p.from); if (v === undefined) { return; } var ks = p.from.split("."), cur = o; for (var i = 0; i < ks.length - 1; i++) { cur = cur[ks[i]]; } delete cur[ks[ks.length - 1]]; __n8nSet(o, p.to, v); });
  rxs.forEach(function (r) { var re = new RegExp(r.search, r.ci ? "gi" : "g"); Object.keys(o).forEach(function (k) { var nk = k.replace(re, r.replace); if (nk !== k) { o[nk] = o[k]; delete o[k]; } }); });
  return o;
});
`, jsValue(pairs), jsValue(rxs)))
}

func mapSummarize(c *MapContext) Mapping {
	c.Ignore("options.disableDotNotation", "options.continueIfFieldNotFound", "options.skipEmptySplitFields")
	type agg struct {
		Agg   string `json:"agg"`
		Field string `json:"field"`
		Sep   string `json:"sep"`
		Empty bool   `json:"empty"`
	}
	list := []agg{}
	for _, v := range c.paramList("fieldsToSummarize.values") {
		a := agg{Agg: "count", Field: fmt.Sprint(v["field"]), Sep: ","}
		if s, ok := v["aggregation"].(string); ok {
			a.Agg = s
		}
		if s, ok := v["separateBy"].(string); ok {
			a.Sep = s
			if s == "other" {
				a.Sep = fmt.Sprint(v["customSeparator"])
			}
		}
		a.Empty, _ = v["includeEmpty"].(bool)
		list = append(list, a)
	}
	split := commaList(c.Literal("fieldsToSplitBy", ""))
	single := c.Literal("options.outputFormat", "separateItems") == "singleItem"
	return jsCode(fmt.Sprintf(`var aggs = %s, split = %s, single = %s, groups = {}, order = [];
var prefix = { append: "appended_", average: "average_", concatenate: "concatenated_", count: "count_", countUnique: "unique_count_", max: "max_", min: "min_", sum: "sum_" };
items.forEach(function (it) {
  var key = JSON.stringify(split.map(function (f) { return __n8nGet(it.json, f); }));
  if (!(key in groups)) { groups[key] = []; order.push(key); }
  groups[key].push(it.json);
});
var out = order.map(function (key) {
  var o = {}, ks = JSON.parse(key);
  split.forEach(function (f, i) { o[f] = ks[i]; });
  aggs.forEach(function (a) {
    var vals = groups[key].map(function (j) { return __n8nGet(j, a.field); });
    if (!a.empty) { vals = vals.filter(function (v) { return v !== undefined && v !== null && v !== ""; }); }
    var nums = vals.map(Number).filter(function (n) { return !isNaN(n); }), r;
    switch (a.agg) {
      case "append": r = vals; break;
      case "concatenate": r = vals.join(a.sep); break;
      case "countUnique": r = Object.keys(vals.reduce(function (m, v) { m[JSON.stringify(v)] = 1; return m; }, {})).length; break;
      case "sum": r = nums.reduce(function (s, n) { return s + n; }, 0); break;
      case "average": r = nums.length ? nums.reduce(function (s, n) { return s + n; }, 0) / nums.length : null; break;
      case "max": r = nums.length ? Math.max.apply(null, nums) : null; break;
      case "min": r = nums.length ? Math.min.apply(null, nums) : null; break;
      default: r = vals.length;
    }
    o[prefix[a.agg] + a.field] = r;
  });
  return o;
});
return single && split.length ? [{ data: out }] : out;
`, jsValue(list), jsValue(split), jsValue(single)))
}

// mapItemLists dispatches the legacy Item Lists node to the standalone
// mappers its operations became.
func mapItemLists(c *MapContext) Mapping {
	c.Ignore("resource")
	switch op := c.Literal("operation", "splitOutItems"); op {
	case "aggregateItems", "concatenateItems":
		return mapAggregate(c)
	case "limit":
		return mapLimit(c)
	case "removeDuplicates":
		return mapRemoveDuplicates(c)
	case "sort":
		return mapSort(c)
	case "splitOutItems":
		return mapSplitOut(c)
	case "summarize":
		return mapSummarize(c)
	default:
		return c.OperationUnsupported("", op)
	}
}
