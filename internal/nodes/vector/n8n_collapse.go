package vector

import (
	"fmt"
	"sort"
	"strings"
)

// N8nSubNode is one node of an n8n sub-node tree, as the importer reads it
// from the workflow's connections.
type N8nSubNode struct {
	Name       string                 // the n8n node's name
	Type       string                 // full n8n type, e.g. "@n8n/n8n-nodes-langchain.embeddingsOpenAi"
	Parameters map[string]interface{} // the node's n8n parameters
	// ConnectionType is how this node attaches to its parent:
	// "ai_embedding", "ai_document", "ai_textSplitter" or "ai_vectorStore".
	ConnectionType string
	// CredentialID is the mono connection the importer chose for this
	// node's n8n credential (see N8nCredentialMappings); "" for none.
	CredentialID string
	Children     []N8nSubNode
}

// N8nConverted is one mono node produced by the mapping.
type N8nConverted struct {
	Type   string
	Config map[string]interface{}
}

// ignoredN8nParams are UI-only parameters that never carry settings.
var ignoredN8nParams = map[string]bool{
	"notice": true, "setupNotice": true, "deprecationNotice": true, "ragStarterCallout": true,
	"includeDocumentMetadata": true, "useReranker": true, "textSplittingMode": true,
	"jsonMode": true, "binaryMode": true, "executeNotice": true,
}

// ConvertN8nParams maps one n8n node's parameters onto mono config, without
// looking at sub-nodes. Unsupported types return an error naming the reason.
func ConvertN8nParams(n8nType string, params map[string]interface{}) (N8nConverted, []string, error) {
	m, ok := N8nNodeMappings[n8nType]
	if !ok {
		return N8nConverted{}, nil, fmt.Errorf("n8n node %s has no RAG mapping", n8nType)
	}
	if m.Coverage == "unsupported" {
		return N8nConverted{}, nil, fmt.Errorf("n8n node %s is not supported: %s", n8nType, m.Notes)
	}
	cfg := map[string]interface{}{}
	for k, v := range m.Fixed {
		cfg[k] = v
	}
	var warnings []string
	if m.Coverage == "partial" && m.Notes != "" {
		warnings = append(warnings, shortType(n8nType)+": "+m.Notes)
	}
	used := map[string]bool{}
	for path, ours := range m.Params {
		v, ok := n8nParam(params, path)
		if !ok || v == nil || v == "" {
			continue
		}
		used[path] = true
		if vals, ok := m.Values[ours]; ok {
			if s, isStr := v.(string); isStr {
				if mapped, ok := vals[s]; ok {
					if s == "epubLoader" {
						warnings = append(warnings, shortType(n8nType)+": EPUB is not supported; loader set to auto")
					}
					v = mapped
				}
			}
		}
		if ours == "mode" {
			if s, isStr := v.(string); isStr {
				if s == "retrieve-as-tool" {
					warnings = append(warnings, shortType(n8nType)+": retrieve-as-tool became a search; give the agent a tool that runs this node")
				}
				v = normalizeMode(s)
			}
		}
		cfg[ours] = v
	}
	for _, leaf := range n8nLeafPaths(params, "") {
		if !used[leaf] && !ignoredN8nParams[strings.TrimPrefix(leaf, "options.")] {
			warnings = append(warnings, fmt.Sprintf("%s: parameter %s is not mapped", shortType(n8nType), leaf))
		}
	}
	sort.Strings(warnings)
	return N8nConverted{Type: m.OurType, Config: cfg}, warnings, nil
}

// n8nParam reads a dotted parameter path, unwrapping resource-locator
// values ({"__rl": true, "mode": "list", "value": "x"}).
func n8nParam(params map[string]interface{}, path string) (interface{}, bool) {
	v, ok := lookupPath(params, path)
	if !ok {
		return nil, false
	}
	if m, isMap := v.(map[string]interface{}); isMap {
		if _, rl := m["__rl"]; rl {
			return m["value"], true
		}
	}
	return v, true
}

// n8nLeafPaths lists the parameter paths that carry values, descending into
// "options" and fixedCollection objects but not into resource locators.
func n8nLeafPaths(params map[string]interface{}, prefix string) []string {
	var out []string
	for k, v := range params {
		p := prefix + k
		if m, isMap := v.(map[string]interface{}); isMap {
			if _, rl := m["__rl"]; !rl && len(m) > 0 {
				out = append(out, n8nLeafPaths(m, p+".")...)
				continue
			}
		}
		if v == nil || v == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func shortType(t string) string { return strings.TrimPrefix(t, N8nPrefix) }
