package apipkg

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

var httpMethods = []string{"get", "post", "put", "patch", "delete", "head"}

var pathParamRE = regexp.MustCompile(`\{([^}]+)\}`)

// operations converts every path operation (filtered by tags).
func (s *spec) operations(p *Package) {
	paths := obj(s.doc["paths"])
	anyTags := false
	for _, path := range sortedKeys(paths) {
		item := s.resolve(paths[path])
		for _, m := range httpMethods {
			if op := obj(item[m]); op != nil {
				if tags, _ := op["tags"].([]any); len(tags) > 0 {
					anyTags = true
				}
			}
		}
	}
	keep := map[string]bool{}
	for _, t := range s.opts.Tags {
		keep[strings.ToLower(strings.TrimSpace(t))] = true
	}
	resources := map[string]string{} // value → label
	var resOrder []string
	used := map[string]bool{}
	for _, path := range sortedKeys(paths) {
		item := s.resolve(paths[path])
		for _, m := range httpMethods {
			raw := obj(item[m])
			if raw == nil {
				continue
			}
			tags, _ := raw["tags"].([]any)
			if len(keep) > 0 && !tagKept(tags, keep) {
				continue
			}
			res, label := "", ""
			if anyTags {
				label = "Default"
				if len(tags) > 0 {
					label = str(tags[0])
				}
				res = camel(label)
				if _, ok := resources[res]; !ok {
					resources[res] = label
					resOrder = append(resOrder, res)
				}
			}
			name := str(raw["operationId"])
			if name == "" {
				name = m + " " + path
			}
			opName := camel(name)
			for i := 2; used[res+"."+opName]; i++ {
				opName = fmt.Sprintf("%s%d", camel(name), i)
			}
			used[res+"."+opName] = true
			op := s.operation(res, opName, strings.ToUpper(m), path, raw, item)
			p.Operations = append(p.Operations, op)
		}
	}
	for _, r := range resOrder {
		p.Resources = append(p.Resources, Resource{Value: r, Label: resources[r]})
	}
}

func tagKept(tags []any, keep map[string]bool) bool {
	for _, t := range tags {
		if keep[strings.ToLower(str(t))] {
			return true
		}
	}
	return false
}

// operation converts one operation object.
func (s *spec) operation(res, name, method, path string, raw, item map[string]any) Operation {
	title := strings.TrimSpace(str(raw["summary"]))
	if title == "" {
		title = method + " " + path
	}
	op := Operation{Resource: res, Operation: name, Title: firstLine(title), Description: firstLine(str(raw["description"])),
		Method: method}
	keys := map[string]bool{}
	addField := func(f workflow.NodeSchemaField) string {
		base := f.Key
		for i := 2; keys[f.Key] || reservedFieldKeys[f.Key]; i++ {
			f.Key = fmt.Sprintf("%s%d", base, i)
		}
		keys[f.Key] = true
		op.Fields = append(op.Fields, f)
		return f.Key
	}

	params := map[string]map[string]any{}
	var order []string
	for _, list := range [][]any{asList(item["parameters"]), asList(raw["parameters"])} {
		for _, pr := range list {
			prm := s.resolve(pr)
			k := str(prm["in"]) + ":" + str(prm["name"])
			if _, ok := params[k]; !ok {
				order = append(order, k)
			}
			params[k] = prm
		}
	}
	pathKeys := map[string]string{}
	var bodyParam map[string]any
	var formParams []map[string]any
	for _, k := range order {
		prm := params[k]
		pname, in := str(prm["name"]), str(prm["in"])
		switch in {
		case "body":
			bodyParam = prm
			continue
		case "formData":
			formParams = append(formParams, prm)
			continue
		case "cookie":
			s.warn("%s %s: cookie parameter %q is skipped", method, path, pname)
			continue
		}
		f := s.schemaToField(camel(pname), pname, s.paramSchema(prm), str(prm["description"]))
		f.Required = in == "path" || prm["required"] == true
		key := addField(f)
		tmpl := "{{ p." + key + " }}"
		switch in {
		case "path":
			pathKeys[pname] = key
		case "query":
			if op.Query == nil {
				op.Query = map[string]any{}
			}
			if explode, ok := prm["explode"].(bool); ok && !explode || str(prm["collectionFormat"]) == "csv" {
				tmpl = "{{ p." + key + " | csv }}"
			}
			op.Query[pname] = tmpl
		case "header":
			if op.Headers == nil {
				op.Headers = map[string]string{}
			}
			op.Headers[pname] = tmpl
		}
	}
	op.Path = pathParamRE.ReplaceAllStringFunc(path, func(m string) string {
		pname := m[1 : len(m)-1]
		key, ok := pathKeys[pname]
		if !ok {
			key = addField(workflow.NodeSchemaField{Key: camel(pname), Label: pname, Type: "text", Required: true})
		}
		return "{{ p." + key + " }}"
	})

	s.requestBody(&op, raw, bodyParam, formParams, addField)
	s.responseBinary(&op, raw, addField)
	return op
}

func asList(v any) []any { l, _ := v.([]any); return l }

func (s *spec) paramSchema(prm map[string]any) map[string]any {
	if sc := s.resolve(prm["schema"]); sc != nil {
		return sc
	}
	return prm // Swagger 2.0 keeps type/enum on the parameter
}

// schemaToField maps a JSON schema to a form field.
func (s *spec) schemaToField(key, label string, sc map[string]any, help string) workflow.NodeSchemaField {
	f := workflow.NodeSchemaField{Key: key, Label: label, Type: "text", Help: firstLine(help)}
	if f.Help == "" {
		f.Help = firstLine(str(sc["description"]))
	}
	if enum, ok := sc["enum"].([]any); ok && len(enum) > 0 {
		f.Type = "select"
		for _, e := range enum {
			f.Options = append(f.Options, stringify(e))
		}
	} else {
		switch schemaType(sc) {
		case "integer", "number":
			f.Type = "number"
		case "boolean":
			f.Type = "boolean"
		case "array":
			f.Type, f.ItemType = "array", "text"
		case "object":
			f.Type = "json"
		}
	}
	if d, ok := sc["default"]; ok {
		f.Default = d
	}
	return f
}

// schemaType is "type", or the first non-null of a 3.1 type array.
func schemaType(sc map[string]any) string {
	switch t := sc["type"].(type) {
	case string:
		return t
	case []any:
		for _, x := range t {
			if str(x) != "null" {
				return str(x)
			}
		}
	}
	if sc["properties"] != nil {
		return "object"
	}
	return ""
}

// properties merges properties and required of a schema and its allOf.
func (s *spec) properties(sc map[string]any) (map[string]any, map[string]bool) {
	props := map[string]any{}
	req := map[string]bool{}
	var walk func(map[string]any, int)
	walk = func(m map[string]any, depth int) {
		if m == nil || depth > 8 {
			return
		}
		for k, v := range obj(m["properties"]) {
			props[k] = v
		}
		for _, r := range asList(m["required"]) {
			req[str(r)] = true
		}
		for _, sub := range asList(m["allOf"]) {
			walk(s.resolve(sub), depth+1)
		}
	}
	walk(sc, 0)
	return props, req
}

// requestBody converts a 3.x requestBody or 2.0 body/formData params.
func (s *spec) requestBody(op *Operation, raw, bodyParam map[string]any, formParams []map[string]any, addField func(workflow.NodeSchemaField) string) {
	var ct string
	var sc map[string]any
	required := false
	if rb := s.resolve(raw["requestBody"]); rb != nil {
		required = rb["required"] == true
		content := obj(rb["content"])
		for _, want := range []string{"application/json", "+json", "application/x-www-form-urlencoded", "multipart/form-data", "application/octet-stream"} {
			for _, k := range sortedKeys(content) {
				if ct == "" && (k == want || (strings.HasPrefix(want, "+") && strings.HasSuffix(k, want))) {
					ct, sc = k, s.resolve(obj(content[k])["schema"])
				}
			}
		}
		if ct == "" && len(content) > 0 {
			ct = sortedKeys(content)[0]
			sc = s.resolve(obj(content[ct])["schema"])
		}
	} else if bodyParam != nil {
		ct, sc, required = "application/json", s.resolve(bodyParam["schema"]), bodyParam["required"] == true
	} else if len(formParams) > 0 {
		ct = "application/x-www-form-urlencoded"
		sc = map[string]any{"type": "object", "properties": map[string]any{}}
		var req []any
		for _, fp := range formParams {
			obj(sc["properties"])[str(fp["name"])] = fp
			if fp["type"] == "file" {
				ct = "multipart/form-data"
			}
			if fp["required"] == true {
				req = append(req, fp["name"])
			}
		}
		sc["required"] = req
	}
	if ct == "" {
		return
	}
	switch {
	case strings.Contains(ct, "json"):
		op.BodyType = BodyJSON
	case ct == "application/x-www-form-urlencoded":
		op.BodyType = BodyForm
	case ct == "multipart/form-data":
		op.BodyType = BodyMultipart
	default:
		op.BodyType = BodyRaw
		key := addField(workflow.NodeSchemaField{Key: "binaryPropertyName", Label: "Input binary property", Type: "text", Default: "data"})
		op.Binary = &Binary{UploadField: key, MimeType: ct}
		return
	}
	props, req := s.properties(sc)
	if len(props) == 0 {
		key := addField(workflow.NodeSchemaField{Key: "body", Label: "Body", Type: "json", Required: required})
		op.Body = "{{ p." + key + " }}"
		return
	}
	body := map[string]any{}
	for _, name := range sortedKeys(props) {
		ps := s.resolve(props[name])
		if ps == nil {
			ps = map[string]any{}
		}
		if op.BodyType == BodyMultipart && (str(ps["format"]) == "binary" || str(ps["type"]) == "file") {
			key := addField(workflow.NodeSchemaField{Key: "binaryPropertyName", Label: "Input binary property", Type: "text", Default: "data"})
			op.Binary = &Binary{UploadField: key, MultipartField: name}
			continue
		}
		f := s.schemaToField(camel(name), name, ps, "")
		f.Required = req[name]
		key := addField(f)
		body[name] = "{{ p." + key + " }}"
	}
	op.Body = body
}

// responseBinary marks an op whose 2xx response is a file as a download.
func (s *spec) responseBinary(op *Operation, raw map[string]any, addField func(workflow.NodeSchemaField) string) {
	for code, rv := range obj(raw["responses"]) {
		if !strings.HasPrefix(code, "2") {
			continue
		}
		r := s.resolve(rv)
		binary := str(s.resolve(r["schema"])["type"]) == "file"
		for ct, c := range obj(r["content"]) {
			if ct == "application/octet-stream" || strings.HasPrefix(ct, "image/") ||
				ct == "application/pdf" || str(s.resolve(obj(c)["schema"])["format"]) == "binary" {
				binary = true
			}
		}
		if binary {
			key := addField(workflow.NodeSchemaField{Key: "binaryPropertyName", Label: "Output binary property", Type: "text", Default: "data"})
			if op.Binary == nil {
				op.Binary = &Binary{}
			}
			op.Binary.Download, op.Binary.PropertyField = true, key
			return
		}
	}
}
