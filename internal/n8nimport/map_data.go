package n8nimport

import (
	"fmt"
	"regexp"
	"strings"
)

func init() {
	Register("n8n-nodes-base.html", mapHTML)
	Register("n8n-nodes-base.htmlExtract", mapHTMLExtract)
	Register("n8n-nodes-base.xml", mapXML)
	Register("n8n-nodes-base.markdown", mapMarkdown)
	Register("n8n-nodes-base.crypto", mapCrypto)
	Register("n8n-nodes-base.dateTime", mapDateTime)
	Register("n8n-nodes-base.compression", mapCompression)
}

var simpleFieldExpr = regexp.MustCompile(`^=\{\{\s*\$json\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}$`)

// valueField returns the item field a value parameter reads. Mono data
// nodes take a field name; a value that is not exactly {{ $json.x }} is
// first written to a helper field by a core.set step (returned in pre).
func (c *MapContext) valueField(param, helper string) (field string, pre *ChainStep) {
	raw := c.Raw(param)
	if s, ok := raw.(string); ok {
		if m := simpleFieldExpr.FindStringSubmatch(s); m != nil {
			return m[1], nil
		}
	}
	v := c.Translate(param, setValueString(raw))
	return helper, &ChainStep{Type: "core.set", Suffix: " (prepare)", Config: map[string]interface{}{
		"assignments":   []interface{}{map[string]interface{}{"field": helper, "value": v, "type": "string"}},
		"include_input": true,
	}}
}

// withPre puts an optional preparing step in front of a mapping.
func withPre(pre *ChainStep, m Mapping) Mapping {
	if pre == nil {
		return m
	}
	main := ChainStep{Type: m.Type, Config: m.Config}
	return Mapping{Type: pre.Type, Config: pre.Config, EntrySuffix: pre.Suffix, Outputs: m.Outputs,
		Chain: append([]ChainStep{main}, m.Chain...)}
}

func mapHTML(c *MapContext) Mapping {
	switch op := c.Literal("operation", "generateHtmlTemplate"); op {
	case "generateHtmlTemplate":
		v := c.Translate("html", setValueString(c.Raw("html")))
		return Mapping{Type: "core.set", Config: map[string]interface{}{
			"assignments":   []interface{}{map[string]interface{}{"field": "html", "value": v, "type": "string"}},
			"include_input": true,
		}}
	case "extractHtmlContent":
		return htmlExtract(c)
	case "convertToHtmlTable":
		c.Ignore("options")
		return jsCode(`var rows = items.map(function (it) { return it.json; });
var cols = []; rows.forEach(function (r) { Object.keys(r).forEach(function (k) { if (cols.indexOf(k) < 0) { cols.push(k); } }); });
function esc(v) { return String(v === undefined || v === null ? "" : (typeof v === "object" ? JSON.stringify(v) : v)).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;"); }
var h = "<table><thead><tr>" + cols.map(function (k) { return "<th>" + esc(k) + "</th>"; }).join("") + "</tr></thead><tbody>";
rows.forEach(function (r) { h += "<tr>" + cols.map(function (k) { return "<td>" + esc(r[k]) + "</td>"; }).join("") + "</tr>"; });
return [{ table: h + "</tbody></table>" }];
`)
	default:
		return c.OperationUnsupported("", op)
	}
}

func mapHTMLExtract(c *MapContext) Mapping { return htmlExtract(c) }

// htmlExtract becomes one data.html step per extraction value.
func htmlExtract(c *MapContext) Mapping {
	if c.Literal("sourceData", "json") == "binary" {
		c.Warn("sourceData", "binary", "data.html reads HTML from an item field, not from binary data")
	}
	field := c.Literal("dataPropertyName", "data")
	c.Ignore("options.trimValues", "options.cleanUpText")
	var steps []ChainStep
	for i, v := range c.paramList("extractionValues.values") {
		cfg := map[string]interface{}{"operation": "extract", "field": field,
			"selector": c.Translate(fmt.Sprintf("extractionValues[%d].cssSelector", i), v["cssSelector"]), "output_field": fmt.Sprint(v["key"])}
		if arr, _ := v["returnArray"].(bool); arr {
			cfg["operation"] = "extract_all"
		}
		switch rv, _ := v["returnValue"].(string); rv {
		case "attribute":
			cfg["attribute"] = fmt.Sprint(v["attribute"])
		case "value":
			cfg["attribute"] = "value"
		case "html":
			c.Warn(fmt.Sprintf("extractionValues[%d].returnValue", i), "html", "data.html returns the element text, not its inner HTML")
		}
		if _, ok := v["skipSelectors"]; ok {
			c.Warn(fmt.Sprintf("extractionValues[%d].skipSelectors", i), v["skipSelectors"], "has no mono-agent equivalent")
		}
		steps = append(steps, ChainStep{Type: "data.html", Suffix: fmt.Sprintf(" (%v)", v["key"]), Config: cfg})
	}
	if len(steps) == 0 {
		return Mapping{Type: "core.merge", Config: map[string]interface{}{}}
	}
	c.Note("the extracted values are added to the item; n8n returned only the extracted values")
	return Mapping{Type: steps[0].Type, Config: steps[0].Config, EntrySuffix: steps[0].Suffix, Chain: steps[1:]}
}

func mapXML(c *MapContext) Mapping {
	field := c.Literal("dataPropertyName", "data")
	if len(c.paramMap("options")) > 0 {
		c.Ignore("options")
		c.Note("xml2js options are not applied; mono-agent uses #text for text and @name for attributes")
	}
	if c.Literal("mode", "xmlToJson") == "jsonToxml" {
		return Mapping{Type: "data.xml", Config: map[string]interface{}{"operation": "generate", "field": field, "output_field": field, "root_element": "root"}}
	}
	return Mapping{Type: "data.xml", Config: map[string]interface{}{"operation": "parse", "field": field, "output_field": field}}
}

func mapMarkdown(c *MapContext) Mapping {
	dest := c.Literal("destinationKey", "data")
	if c.Literal("mode", "htmlToMarkdown") == "htmlToMarkdown" {
		c.Ignore("html", "options")
		return c.OperationUnsupported("", "htmlToMarkdown")
	}
	c.Ignore("options")
	field, pre := c.valueField("markdown", "__n8n_markdown")
	if pre != nil {
		c.Warn("markdown", nil, "items carry the helper field __n8n_markdown")
	}
	return withPre(pre, Mapping{Type: "data.markdown", Config: map[string]interface{}{"field": field, "output_field": dest}})
}

var hashOps = map[string]string{"MD5": "hash_md5", "SHA256": "hash_sha256", "SHA512": "hash_sha512"}

func mapCrypto(c *MapContext) Mapping {
	action := c.Literal("action", "hash")
	out := c.Literal("dataPropertyName", "data")
	switch action {
	case "generate":
		switch enc := c.Literal("encodingType", "uuid"); enc {
		case "uuid":
			return Mapping{Type: "data.crypto", Config: map[string]interface{}{"operation": "uuid", "output_field": out}}
		case "hex", "base64":
			n := c.Float("stringLength", 32)
			bytes := n / 2
			if enc == "base64" {
				bytes = n * 3 / 4
			}
			c.Note("the generated string length is approximate (%v random bytes)", int(bytes))
			return Mapping{Type: "data.crypto", Config: map[string]interface{}{"operation": "random_bytes", "encoding": enc, "length": float64(int(bytes)), "output_field": out}}
		default:
			c.Ignore("stringLength")
			return c.OperationUnsupported("", "generate "+enc)
		}
	case "hash", "hmac":
		typ := c.Literal("type", map[bool]string{true: "SHA256", false: "MD5"}[c.version(2)])
		op := hashOps[typ]
		if action == "hmac" {
			op = map[string]string{"SHA256": "hmac_sha256"}[typ]
		}
		if op == "" {
			return c.OperationUnsupported("", action+" "+typ)
		}
		if c.Bool("binaryData", false) {
			c.Warn("binaryData", true, "data.crypto hashes item fields, not binary data")
			c.Ignore("binaryPropertyName")
		}
		field, pre := c.valueField("value", "__n8n_value")
		cfg := map[string]interface{}{"operation": op, "field": field, "output_field": out, "encoding": c.Literal("encoding", "hex")}
		if action == "hmac" {
			if c.Has("secret") {
				cfg["secret"] = c.Str("secret", "")
			} else {
				cfg["secret"] = c.Secret("secret", "crypto")
			}
		}
		if pre != nil {
			c.Warn("value", nil, "items carry the helper field __n8n_value")
		}
		return withPre(pre, Mapping{Type: "data.crypto", Config: cfg})
	}
	return c.OperationUnsupported("", action)
}

func mapCompression(c *MapContext) Mapping {
	field := c.Literal("binaryPropertyName", "data")
	c.Warn("binaryPropertyName", field, "data.compression works on base64 text in item fields, not on n8n binary data")
	c.Ignore("binaryPropertyOutput", "outputPrefix", "fileName")
	if c.Literal("operation", "decompress") == "compress" {
		def := ""
		if c.version(1.1) {
			def = "zip"
		}
		format := c.Literal("outputFormat", def)
		op := map[string]string{"zip": "zip", "gzip": "gzip", "": "zip"}[format]
		if op == "" {
			return c.OperationUnsupported("", "compress "+format)
		}
		return Mapping{Type: "data.compression", Config: map[string]interface{}{"operation": op, "input_field": field, "output_field": field, "filename": c.Str("fileName", "data")}}
	}
	c.Note("decompression assumes zip; set operation to gunzip for .gz data")
	return Mapping{Type: "data.compression", Config: map[string]interface{}{"operation": "unzip", "input_field": field, "output_field": field}}
}

// mapDateTime: the current date maps to data.datetime; the date math and
// formatting run in JavaScript with the Luxon-like helpers of the code
// prelude, which understand n8n's formats.
func mapDateTime(c *MapContext) Mapping {
	c.Ignore("options.includeInputFields", "options.timezone", "options.fromFormat", "options.fromTimezone", "options.toTimezone")
	if c.Has("options.timezone") || c.Has("options.toTimezone") {
		c.Note("dates are computed in UTC")
	}
	if !c.version(2) {
		return dateTimeV1(c)
	}
	op := c.Literal("operation", "getCurrentDate")
	out := func(def string) string { return c.Literal("outputFieldName", def) }
	val := func(param string) string {
		js, ok := jsForValue(c.Raw(param))
		if !ok {
			c.Warn(param, c.Raw(param), "reads data a code step cannot reach")
			return "undefined"
		}
		return js
	}
	var expr, field string
	switch op {
	case "getCurrentDate":
		field = out("currentDate")
		expr = "DateTime.now().toISO()"
		if !c.Bool("includeTime", true) {
			expr = "DateTime.now().toISODate()"
		}
	case "addToDate", "subtractFromDate":
		field = out("newDate")
		fn := map[string]string{"addToDate": "plus", "subtractFromDate": "minus"}[op]
		expr = fmt.Sprintf("DateTime.fromISO(%s).%s({ %s: Number(%s) }).toISO()", val("magnitude"), fn, jsQuote(c.Literal("timeUnit", "days")), val("duration"))
	case "formatDate":
		field = out("formattedDate")
		f := c.Literal("format", "MM/dd/yyyy")
		if f == "custom" {
			f = c.Literal("customFormat", "")
		}
		switch f {
		case "X":
			expr = fmt.Sprintf("DateTime.fromISO(%s).toUnixInteger()", val("date"))
		case "x":
			expr = fmt.Sprintf("DateTime.fromISO(%s).toMillis()", val("date"))
		default:
			expr = fmt.Sprintf("DateTime.fromISO(%s).toFormat(%s)", val("date"), jsQuote(f))
			if strings.ContainsAny(strings.Trim(f, "yMdHms'-/:. T,"), "abcefghijklnopqrtuvwzEGLSWZ") {
				c.Note("format %q uses Luxon tokens beyond yyyy MM dd HH mm ss; check the output", f)
			}
		}
	case "getTimeBetweenDates":
		field = out("timeDifference")
		expr = fmt.Sprintf("(function (a, b, units) { var ms = new Date(b).getTime() - new Date(a).getTime(); var size = { year: 31536e6, month: 2592e6, week: 6048e5, day: 864e5, hour: 36e5, minute: 6e4, second: 1e3, millisecond: 1 }; var o = {}; units.forEach(function (u) { o[u + 's'] = ms / size[u]; }); return o; })(%s, %s, %s)",
			val("startDate"), val("endDate"), jsValue(c.Raw("units")))
		if c.Raw("units") == nil {
			expr = strings.Replace(expr, ", null)", `, ["day"])`, 1)
		}
		c.Ignore("options.isoString")
	case "extractDate":
		field = out("datePart")
		part := c.Literal("part", "month")
		expr = fmt.Sprintf("DateTime.fromISO(%s)[%s]", val("date"), jsQuote(part))
		if part == "week" {
			c.Warn("part", part, "week numbers are not computed")
		}
	case "roundDate":
		field = out("roundedDate")
		unit := c.Literal("toNearest", "month")
		if c.Literal("mode", "roundDown") == "roundUp" {
			c.Warn("mode", "roundUp", "rounds down instead")
			c.Ignore("to")
		}
		expr = fmt.Sprintf("DateTime.fromISO(%s).startOf(%s).toISO()", val("date"), jsQuote(unit))
	default:
		return c.OperationUnsupported("", op)
	}
	body := jsValueHelpers + "var o = Object.assign({}, $json);\no[" + jsQuote(field) + "] = " + expr + ";\nreturn o;\n"
	return Mapping{Type: "core.code", Config: codeConfig(wrapPerItem(body))}
}

func dateTimeV1(c *MapContext) Mapping {
	field := c.Literal("dataPropertyName", "data")
	js, ok := jsForValue(c.Raw("value"))
	if !ok {
		js = "undefined"
		c.Warn("value", c.Raw("value"), "reads data a code step cannot reach")
	}
	var expr string
	if c.Literal("action", "format") == "calculate" {
		fn := map[string]string{"add": "plus", "subtract": "minus"}[c.Literal("operation", "add")]
		expr = fmt.Sprintf("DateTime.fromISO(%s).%s({ %s: Number(%s) }).toISO()", js, fn, jsQuote(c.Literal("timeUnit", "days")), jsValue(c.Float("duration", 0)))
	} else {
		f := c.Literal("toFormat", "")
		moment := strings.NewReplacer("YYYY", "yyyy", "DD", "dd")
		expr = fmt.Sprintf("DateTime.fromISO(%s).toFormat(%s)", js, jsQuote(moment.Replace(f)))
		c.Ignore("custom")
		if f == "" {
			expr = fmt.Sprintf("DateTime.fromISO(%s).toISO()", js)
		}
	}
	body := jsValueHelpers + "var o = Object.assign({}, $json);\n__n8nSet(o, " + jsQuote(field) + ", " + expr + ");\nreturn o;\n"
	return Mapping{Type: "core.code", Config: codeConfig(wrapPerItem(body))}
}
