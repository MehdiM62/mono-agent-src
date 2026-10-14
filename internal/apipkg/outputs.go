package apipkg

import (
	"fmt"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// responseOutputKey is the output key of an op whose items carry the API's
// own response fields. Like every "<…>" key that names no config field it
// is an open key set: the validator does not judge $json references to it.
const responseOutputKey = "<API response fields>"

// outputTypes are the types a declared output may have (the node schemas').
var outputTypes = map[string]bool{
	"": true, "string": true, "number": true, "boolean": true, "object": true, "array": true, "any": true,
}

// maxNamedOps caps how many operations an output's description names.
const maxNamedOps = 8

func responseOutput() workflow.NodeSchemaOutput {
	return workflow.NodeSchemaOutput{Key: responseOutputKey, Type: "any",
		Description: "the API's JSON response (one item per array element when the operation lists)"}
}

// opOutputs is what an op's items carry: its declared outputs, else the
// package's, else what its binary and output settings imply.
func (p *Package) opOutputs(o *Operation) []workflow.NodeSchemaOutput {
	if o.Outputs != nil {
		return o.Outputs
	}
	if p.Outputs != nil {
		return p.Outputs
	}
	return deriveOutputs(o)
}

// deriveOutputs reads an op's outputs off its settings: a file download
// gives the download item's keys, output.to fixed keys, anything else the
// open response key. Pagination adds pagination_truncated.
func deriveOutputs(o *Operation) []workflow.NodeSchemaOutput {
	var outs []workflow.NodeSchemaOutput
	add := func(xs ...workflow.NodeSchemaOutput) {
		for _, x := range xs {
			if !hasOutput(outs, x.Key) {
				outs = append(outs, x)
			}
		}
	}
	b := o.Binary
	file := b != nil && (b.Download || b.DownloadWhen != "")
	switch {
	case file && b.DownloadBase64 != "":
		// The JSON response stays the item's json beside the file.
		add(responseOutput())
	case file:
		add(downloadOutputs()...)
	}
	if !file || b.DownloadWhen != "" {
		add(shapedOutputs(o.Output)...)
		if o.Pagination != nil {
			add(workflow.NodeSchemaOutput{Key: "pagination_truncated", Type: "boolean",
				Description: "true on every item when the run stopped at max_pages before the list ended"})
		}
	}
	if o.Bulk != nil {
		add(shapedOutputs(o.Bulk.Output)...)
	}
	return outs
}

// downloadOutputs are the keys of a file download's item (downloadItem).
func downloadOutputs() []workflow.NodeSchemaOutput {
	return []workflow.NodeSchemaOutput{
		{Key: "mime_type", Type: "string", Description: "the downloaded file's MIME type"},
		{Key: "file_name", Type: "string", Description: "the file name the response gives, if any"},
		{Key: "size", Type: "number", Description: "the file size in bytes"},
		{Key: "binary_property", Type: "string", Description: "the binary property holding the file"},
	}
}

// shapedOutputs are the keys output.to builds when it always applies (plus
// data, for a scalar response it leaves alone);
// otherwise (and beside builder keys like $merge) the response is open.
func shapedOutputs(out *Output) []workflow.NodeSchemaOutput {
	if out == nil || out.ToWhen != "" {
		return []workflow.NodeSchemaOutput{responseOutput()}
	}
	to, ok := out.To.(map[string]any)
	if !ok || len(to) == 0 {
		return []workflow.NodeSchemaOutput{responseOutput()}
	}
	var outs []workflow.NodeSchemaOutput
	open := false
	for _, k := range sortedKeys(to) {
		if strings.HasPrefix(k, "$") {
			open = true
			continue
		}
		// "!key" is sent even when empty; the item key is key.
		outs = append(outs, workflow.NodeSchemaOutput{Key: strings.TrimPrefix(k, "!"), Type: valueType(to[k])})
	}
	// output.to reshapes objects and list elements; a scalar response
	// passes through as {"data": v}.
	if _, taken := to["data"]; !taken {
		outs = append(outs, workflow.NodeSchemaOutput{Key: "data", Type: "any",
			Description: "the response itself when the API answers with a single value instead of an object or list"})
	}
	if open {
		outs = append(outs, responseOutput())
	}
	return outs
}

// valueType is the output type of an output.to value.
func valueType(v any) string {
	switch x := v.(type) {
	case string:
		if hasTemplate(x) {
			return "any"
		}
		return "string"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "any"
}

func hasOutput(outs []workflow.NodeSchemaOutput, key string) bool {
	for _, o := range outs {
		if o.Key == key {
			return true
		}
	}
	return false
}

// schemaOutputs merges every op's outputs into the node schema's list:
// one entry per (key, type, description), with visible_for naming the ops
// that emit it. Node schema outputs don't vary by operation, so an entry
// some ops lack names them in its description; partial (an extension)
// always names them, since the native operations emit none of these.
func (p *Package) schemaOutputs(partial bool) []map[string]any {
	type group struct {
		out workflow.NodeSchemaOutput
		ops []opRef
	}
	var order []string
	groups := map[string]*group{}
	for i := range p.Operations {
		o := &p.Operations[i]
		for _, out := range p.opOutputs(o) {
			id := out.Key + "\x00" + out.Type + "\x00" + out.Description
			g, ok := groups[id]
			if !ok {
				g = &group{out: out}
				groups[id] = g
				order = append(order, id)
			}
			g.ops = append(g.ops, opRef{o.Resource, o.Operation})
		}
	}
	res := make([]map[string]any, 0, len(order))
	for _, id := range order {
		g := groups[id]
		m := map[string]any{"key": g.out.Key, "visible_for": g.ops}
		if g.out.Type != "" {
			m["type"] = g.out.Type
		}
		desc := g.out.Description
		if partial || len(g.ops) < len(p.Operations) {
			desc = joinDesc(desc, "operations: "+opList(g.ops))
		}
		if desc != "" {
			m["description"] = desc
		}
		res = append(res, m)
	}
	return res
}

func joinDesc(desc, more string) string {
	if desc == "" {
		return more
	}
	return desc + " — " + more
}

// opList names ops as resource.operation, at most maxNamedOps of them.
func opList(ops []opRef) string {
	var names []string
	for i, o := range ops {
		if i == maxNamedOps {
			names = append(names, fmt.Sprintf("and %d more (see visible_for)", len(ops)-maxNamedOps))
			break
		}
		if o.Resource == "" {
			names = append(names, o.Operation)
		} else {
			names = append(names, o.Resource+"."+o.Operation)
		}
	}
	return strings.Join(names, ", ")
}

// checkOutputDecls checks a declared outputs list.
func checkOutputDecls(where string, outs []workflow.NodeSchemaOutput) []string {
	var probs []string
	seen := map[string]bool{}
	for i, o := range outs {
		if strings.TrimSpace(o.Key) == "" {
			probs = append(probs, fmt.Sprintf("%s[%d]: key is empty", where, i))
		}
		if !outputTypes[o.Type] {
			probs = append(probs, fmt.Sprintf("%s[%d] (%s): type %q is not one of string, number, boolean, object, array, any", where, i, o.Key, o.Type))
		}
		if seen[o.Key] {
			probs = append(probs, fmt.Sprintf("%s[%d]: key %q is listed twice", where, i, o.Key))
		}
		seen[o.Key] = true
	}
	return probs
}

// extendOutputs appends the package's outputs to a native schema's,
// skipping keys the native node already declares.
func (p *Package) extendOutputs(doc map[string]any) {
	outs, _ := doc["outputs"].([]any)
	have := map[string]bool{}
	for _, o := range outs {
		if m, ok := o.(map[string]any); ok {
			k, _ := m["key"].(string)
			have[k] = true
		}
	}
	for _, m := range p.schemaOutputs(true) {
		if k := m["key"].(string); !have[k] {
			have[k] = true
			outs = append(outs, m)
		}
	}
	if len(outs) > 0 {
		doc["outputs"] = outs
	}
}
