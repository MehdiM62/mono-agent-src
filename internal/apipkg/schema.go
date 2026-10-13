package apipkg

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// opRef names one op a schema field is shown for.
type opRef struct {
	Resource  string `json:"resource"`
	Operation string `json:"operation"`
}

// SchemaFor returns the node schema JSON of a package node type ("app.x"),
// or of a native node a package extends (the native schema file with the
// package's fields appended). false when no package answers for nodeType.
func SchemaFor(nodeType string) ([]byte, bool) {
	if p, ok := ForNodeType(nodeType); ok {
		return p.nodeSchema()
	}
	if extensionFor(nodeType) != nil {
		// The registered provider merges the package into the native schema.
		return workflow.ReadEmbeddedSchema(nodeType)
	}
	return nil, false
}

// schemaProvider is registered with workflow.RegisterSchemaProvider.
func schemaProvider(nodeType string, fileSchema []byte) ([]byte, bool) {
	if p, ok := ForNodeType(nodeType); ok {
		return p.nodeSchema()
	}
	if p := extensionFor(nodeType); p != nil {
		base := fileSchema
		if base == nil {
			base = []byte(`{"credential_platform":null,"fields":[]}`)
		}
		return p.extendSchema(base)
	}
	return nil, false
}

// nodeSchema builds a package node's schema: connection, resource,
// operation (one select per resource) and every op's fields.
func (p *Package) nodeSchema() ([]byte, bool) {
	var fields []map[string]any
	fields = append(fields, map[string]any{
		"key": "credential_id", "label": p.Title + " Connection", "type": "credential_picker",
		"required": p.needsCredential(),
	})
	fields = append(fields, p.selectorFields(false)...)
	fields = append(fields, p.opFields()...)
	doc := map[string]any{
		"title":               p.Title,
		"description":         p.Description,
		"category":            p.categoryOrApp(),
		"apipkg":              p.ID,
		"credential_platform": nil,
		"fields":              fields,
	}
	if p.DocsURL != "" {
		doc["docs_url"] = p.DocsURL
	}
	if pl := p.PrimaryPlatform(); pl != "" {
		doc["credential_platform"] = pl
	}
	b, err := json.Marshal(doc)
	return b, err == nil
}

func (p *Package) categoryOrApp() string {
	if p.Category != "" {
		return p.Category
	}
	return "app"
}

// selectorFields are the resource select and the operation selects. For an
// extension the resource select starts with "" (the native operations).
func (p *Package) selectorFields(extension bool) []map[string]any {
	var out []map[string]any
	if len(p.Resources) == 0 {
		return []map[string]any{{
			"key": "operation", "label": "Operation", "type": "select", "required": true,
			"options": p.opNames(""), "default": p.DefaultOperation(""),
		}}
	}
	var vals []string
	if extension {
		vals = append(vals, "")
	}
	for _, r := range p.Resources {
		vals = append(vals, r.Value)
	}
	res := map[string]any{"key": "resource", "label": "Resource", "type": "select", "required": !extension, "options": vals}
	if extension {
		res["default"] = ""
		res["help"] = "Leave empty for the built-in operations; pick a resource for the " + p.Title + " API package operations."
	} else {
		res["default"] = p.DefaultResource()
	}
	out = append(out, res)
	for _, r := range p.Resources {
		out = append(out, map[string]any{
			"key": "operation", "label": r.Label + " Operation", "type": "select", "required": true,
			"options": p.opNames(r.Value), "default": p.DefaultOperation(r.Value),
			"depends_on": map[string]any{"key": "resource", "values": []string{r.Value}},
		})
	}
	return out
}

func (p *Package) opNames(resource string) []string {
	var out []string
	for _, o := range p.Operations {
		if o.Resource == resource {
			out = append(out, o.Operation)
		}
	}
	return out
}

// opFields merges every op's fields: one entry per (key, type, label, options),
// shown for the ops listed in visible_for. depends_on carries the operation
// names (the editor's single-key rule); visible_for is exact.
func (p *Package) opFields() []map[string]any {
	type group struct {
		field    workflow.NodeSchemaField
		required bool
		ops      []opRef
		opNames  []string
	}
	var order []string
	groups := map[string]*group{}
	for _, o := range p.Operations {
		for _, f := range o.Fields {
			id := f.Key + "\x00" + f.Type + "\x00" + f.Label + "\x00" + strings.Join(f.Options, ",")
			g, ok := groups[id]
			if !ok {
				g = &group{field: f, required: f.Required}
				groups[id] = g
				order = append(order, id)
			}
			g.required = g.required && f.Required
			g.ops = append(g.ops, opRef{o.Resource, o.Operation})
			if !contains(g.opNames, o.Operation) {
				g.opNames = append(g.opNames, o.Operation)
			}
		}
	}
	out := make([]map[string]any, 0, len(order))
	for _, id := range order {
		g := groups[id]
		f := schemaField(g.field)
		f.Required = g.required
		f.DependsOn = &workflow.FieldDependency{Key: "operation", Values: g.opNames}
		m := toMap(f)
		m["visible_for"] = g.ops
		out = append(out, m)
	}
	return out
}

// schemaField maps package field types to the editor's.
func schemaField(f workflow.NodeSchemaField) workflow.NodeSchemaField {
	switch f.Type {
	case "collection", "json":
		if f.Type == "collection" && len(f.Options) > 0 {
			keys := "Keys: " + strings.Join(f.Options, ", ")
			if f.Help != "" {
				keys = f.Help + " " + keys
			}
			f.Help = keys
			f.Options = nil
		}
		f.Type, f.Language = "code", "json"
	case "multiselect":
		f.Type = "array"
		if f.ItemType == "" {
			f.ItemType = "text"
		}
	case "datetime":
		f.Type = "text"
		if f.Placeholder == "" {
			f.Placeholder = "2026-01-31T09:00:00Z"
		}
	}
	return f
}

func toMap(f workflow.NodeSchemaField) map[string]any {
	b, _ := json.Marshal(f)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// extendSchema appends the package's selectors and fields to a native
// node's schema. Native fields stay first. Without resources, the package's
// operations are appended to the native "operation" select's options.
func (p *Package) extendSchema(base []byte) ([]byte, bool) {
	var doc map[string]any
	if err := json.Unmarshal(base, &doc); err != nil {
		return nil, false
	}
	fields, _ := doc["fields"].([]any)
	if len(p.Resources) == 0 {
		merged := false
		for _, f := range fields {
			m, ok := f.(map[string]any)
			if !ok || m["key"] != "operation" {
				continue
			}
			opts, _ := m["options"].([]any)
			for _, name := range p.opNames("") {
				if !containsAny(opts, name) {
					opts = append(opts, name)
				}
			}
			m["options"] = opts
			merged = true
		}
		if !merged {
			for _, f := range p.selectorFields(true) {
				fields = append(fields, f)
			}
		}
	} else {
		for _, f := range p.selectorFields(true) {
			fields = append(fields, f)
		}
	}
	for _, f := range p.opFields() {
		fields = append(fields, f)
	}
	doc["fields"] = fields
	exts, _ := doc["extended_by"].([]any)
	doc["extended_by"] = append(exts, p.ID)
	b, err := json.Marshal(doc)
	return b, err == nil
}

func containsAny(xs []any, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// sortedKeys returns m's keys sorted.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
