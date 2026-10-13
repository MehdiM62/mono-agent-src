package n8nimport

import (
	"fmt"
	"sort"
	"strings"

	vectornodes "github.com/monoes/mono-agent/internal/nodes/vector"
)

// n8n LangChain vector stores and document loaders in the main flow become
// vector.* nodes: internal/nodes/vector folds their embedding, document
// loader and text splitter sub-nodes into flat config
// (CollapseN8nSubNodes). Embeddings, splitters and retrievers only appear
// as sub-nodes and are folded into their parent.
func init() {
	for t, m := range vectornodes.N8nNodeMappings {
		if m.Role == vectornodes.N8nRoleNode {
			Register(t, mapVector)
		}
	}
}

func mapVector(c *MapContext) Mapping {
	for k := range c.Params {
		c.Ignore(k) // the collapse reports unmapped parameters itself
	}
	res, err := vectornodes.CollapseN8nSubNodes(c.Type, withoutEmpty(c.Params), c.vectorSubs(c.SubNodes))
	if err != nil {
		c.issues = append(c.issues, Issue{Severity: SevError, Message: err.Error()})
		return Mapping{Unmapped: true}
	}
	for _, w := range res.Warnings {
		c.Note("%s", w)
	}
	node := res.Node
	node.Config = c.translateConfig(node.Config)
	if platform, credType := vectorCredential(c.Credentials); platform != "" {
		node.Config["credential_id"] = c.Credential(platform, credType)
		c.creds[len(c.creds)-1].Hint = vectorHint(platform, credType)
	}
	if len(res.Before) == 0 {
		return Mapping{Type: node.Type, Config: node.Config}
	}
	// Loaders run first: incoming items reach the first one, the store
	// keeps the n8n name and its outgoing connections.
	first := res.Before[0]
	var chain []ChainStep
	for i, b := range res.Before[1:] {
		chain = append(chain, ChainStep{Type: b.Type, Suffix: fmt.Sprintf(" (step %d)", i+2), Config: c.translateConfig(b.Config)})
	}
	chain = append(chain, ChainStep{Type: node.Type, Config: node.Config})
	return Mapping{Type: first.Type, Config: c.translateConfig(first.Config), EntrySuffix: " (load documents)", Chain: chain}
}

// translateConfig translates n8n expressions left in collapsed config.
func (c *MapContext) translateConfig(cfg map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(cfg))
	for k, v := range cfg {
		out[k] = c.Translate(k, v)
	}
	return out
}

// vectorSubs converts sub-nodes to the vector package's tree, choosing
// each one's connection from its n8n credential.
func (c *MapContext) vectorSubs(subs []SubNode) []vectornodes.N8nSubNode {
	out := make([]vectornodes.N8nSubNode, 0, len(subs))
	for _, s := range subs {
		n := vectornodes.N8nSubNode{Type: s.Type, Parameters: withoutEmpty(s.Parameters), ConnectionType: s.Kind,
			Children: c.vectorSubs(s.Children)}
		if platform, credType := vectorCredential(s.Credentials); platform != "" {
			n.CredentialID = platform
			c.creds = append(c.creds, credUse{Platform: platform, N8nType: credType,
				N8nName: s.Credentials[credType].Name, Hint: vectorHint(platform, credType)})
		}
		out = append(out, n)
	}
	return out
}

// withoutEmpty drops empty objects (n8n saves "options": {}), which carry
// no setting.
func withoutEmpty(params map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(params))
	for k, v := range params {
		if m, ok := v.(map[string]interface{}); ok && len(m) == 0 {
			continue
		}
		out[k] = v
	}
	return out
}

// vectorCredential picks the first n8n credential with a RAG mapping.
func vectorCredential(creds map[string]n8nCredentialRef) (platform, credType string) {
	for _, t := range sortedKeys(creds) {
		if m, ok := vectornodes.N8nCredentialMappings[t]; ok {
			return m.Platform, t
		}
	}
	return "", ""
}

func vectorHint(platform, credType string) string {
	hint := "monoagentcli connect " + platform
	var renames []string
	for n8n, ours := range vectornodes.N8nCredentialMappings[credType].Fields {
		if n8n != ours {
			renames = append(renames, n8n+" → "+ours)
		}
	}
	if len(renames) > 0 {
		sort.Strings(renames)
		hint += "; n8n credential fields: " + strings.Join(renames, ", ")
	}
	return hint
}
