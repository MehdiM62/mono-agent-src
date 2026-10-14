package n8nimport

import (
	"sort"
	"strings"

	vectornodes "github.com/monoes/mono-agent/internal/nodes/vector"
)

// n8n LangChain vector stores and document loaders in the main flow become
// vector.* nodes; their embedding, document loader and text splitter
// sub-nodes become vector.embeddings / vector.document_loader /
// vector.text_splitter sub-nodes attached through n8n's connection types
// (internal/nodes/vector ConvertN8nTree). Retrievers only appear as
// sub-nodes and become a search of their vector store.
func init() {
	for t, m := range vectornodes.N8nNodeMappings {
		if m.Role == vectornodes.N8nRoleNode {
			Register(t, mapVector)
		}
	}
}

func mapVector(c *MapContext) Mapping {
	for k := range c.Params {
		c.Ignore(k) // the conversion reports unmapped parameters itself
	}
	res, err := vectornodes.ConvertN8nTree(c.Type, withoutEmpty(c.Params), c.vectorSubs(c.SubNodes))
	if err != nil {
		c.issues = append(c.issues, Issue{Severity: SevError, Message: err.Error()})
		return Mapping{Unmapped: true}
	}
	for _, w := range res.Warnings {
		c.Note("%s", w)
	}
	cfg := c.translateConfig(res.Node.Config)
	if platform, credType := vectorCredential(c.Credentials); platform != "" {
		cfg["credential_id"] = c.Credential(platform, credType)
		c.creds[len(c.creds)-1].Hint = vectorHint(platform, credType)
	}
	c.attachVector(res.Attached, cfg)
	return Mapping{Type: res.Node.Type, Config: cfg}
}

// attachVector keeps converted RAG sub-nodes (and theirs) as sub-nodes of
// the node whose config is target.
func (c *MapContext) attachVector(list []vectornodes.N8nAttached, target map[string]interface{}) {
	for _, a := range list {
		cfg := c.translateConfig(a.Node.Config)
		c.attach(a.Name, a.Kind, a.Node.Type, cfg, target)
		c.attachVector(a.Children, cfg)
	}
}

// searchSubs keeps only the embeddings of a vector store that is searched
// (by a retriever or a tool), noting a document loader it does not use.
func (c *MapContext) searchSubs(store string, list []vectornodes.N8nAttached) []vectornodes.N8nAttached {
	var out []vectornodes.N8nAttached
	for _, a := range list {
		if a.Kind == vectornodes.KindEmbedding {
			out = append(out, a)
		} else {
			c.Note("the document loader attached to %q is not used: it is only searched", store)
		}
	}
	return out
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
		n := vectornodes.N8nSubNode{Name: s.Name, Type: s.Type, Parameters: withoutEmpty(s.Parameters), ConnectionType: s.Kind,
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
