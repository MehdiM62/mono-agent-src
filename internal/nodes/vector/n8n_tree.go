package vector

import "fmt"

// N8nAttached is an n8n sub-node mapped 1:1 to a mono sub-node, attached
// to its parent through a Kind connection ("ai_embedding", "ai_document",
// "ai_textSplitter").
type N8nAttached struct {
	Name     string // the n8n node's name (N8nSubNode.Name)
	Kind     string
	Node     N8nConverted
	Children []N8nAttached
}

// N8nTree is an n8n root node with its embeddings, document loader and
// text splitter sub-nodes kept as mono sub-nodes.
type N8nTree struct {
	Node     N8nConverted
	Attached []N8nAttached
	Warnings []string
}

// ConvertN8nTree maps an n8n vector store or document loader and its
// sub-nodes 1:1: a store's embeddings become a vector.embeddings sub-node
// (ai_embedding) and its document loader a vector.document_loader
// (ai_document); a loader's text splitter becomes a vector.text_splitter
// (ai_textSplitter). A retriever is mapped as its ai_vectorStore child
// searching, with the child's sub-nodes. Unlike CollapseN8nSubNodes nothing
// is folded into config or inserted before the root.
func ConvertN8nTree(rootType string, params map[string]interface{}, subs []N8nSubNode) (N8nTree, error) {
	var res N8nTree
	m, ok := N8nNodeMappings[rootType]
	if !ok {
		return res, fmt.Errorf("n8n node %s has no RAG mapping", rootType)
	}
	if m.Role == N8nRoleRetriever {
		return convertRetrieverTree(rootType, params, subs)
	}
	conv, warns, err := ConvertN8nParams(rootType, params)
	if err != nil {
		return res, err
	}
	res.Node, res.Warnings = conv, warns
	if conv.Type == "vector.load_document" {
		res.Attached = loaderSubs(&res.Warnings, conv.Config, params, subs)
		return res, nil
	}
	for _, sub := range subs {
		switch {
		case sub.ConnectionType == KindEmbedding && !hasKind(res.Attached, KindEmbedding):
			emb, w, err := ConvertN8nParams(sub.Type, sub.Parameters)
			res.Warnings = append(res.Warnings, w...)
			if err != nil {
				res.Warnings = append(res.Warnings, err.Error())
				continue
			}
			if sub.CredentialID != "" {
				emb.Config["credential_id"] = sub.CredentialID
			}
			res.Attached = append(res.Attached, N8nAttached{Name: sub.Name, Kind: KindEmbedding,
				Node: N8nConverted{Type: TypeEmbeddings, Config: emb.Config}})
		case sub.ConnectionType == KindDocument && !hasKind(res.Attached, KindDocument):
			loader, w, err := ConvertN8nParams(sub.Type, sub.Parameters)
			res.Warnings = append(res.Warnings, w...)
			if err != nil || loader.Type != "vector.load_document" {
				if err == nil {
					err = fmt.Errorf("%s is not a document loader", shortType(sub.Type))
				}
				res.Warnings = append(res.Warnings, err.Error())
				continue
			}
			children := loaderSubs(&res.Warnings, loader.Config, sub.Parameters, sub.Children)
			res.Attached = append(res.Attached, N8nAttached{Name: sub.Name, Kind: KindDocument,
				Node: N8nConverted{Type: TypeDocumentLoader, Config: loader.Config}, Children: children})
		default:
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s sub-node %s is not used", shortType(rootType), sub.ConnectionType, shortType(sub.Type)))
		}
	}
	if !hasKind(res.Attached, KindEmbedding) && conv.Type == "vector.store" {
		res.Warnings = append(res.Warnings, shortType(rootType)+": no embedding sub-node; items must carry precomputed vectors in \"embedding\"")
	}
	return res, nil
}

// loaderSubs maps a loader's text splitter to a vector.text_splitter
// sub-node. Without one, n8n's "simple" splitting mode is set on cfg.
func loaderSubs(warnings *[]string, cfg, params map[string]interface{}, subs []N8nSubNode) []N8nAttached {
	for _, sub := range subs {
		if sub.ConnectionType != KindTextSplitter {
			*warnings = append(*warnings, fmt.Sprintf("loader: %s sub-node %s is not used", sub.ConnectionType, shortType(sub.Type)))
			continue
		}
		sp, w, err := ConvertN8nParams(sub.Type, sub.Parameters)
		*warnings = append(*warnings, w...)
		if err != nil {
			*warnings = append(*warnings, err.Error())
			continue
		}
		return []N8nAttached{{Name: sub.Name, Kind: KindTextSplitter, Node: N8nConverted{Type: TypeTextSplitter, Config: sp.Config}}}
	}
	if mode, _ := params["textSplittingMode"].(string); mode == "simple" {
		cfg["split"] = true
		cfg["splitter"] = SplitterRecursive
		cfg["chunk_size"] = 1000
		cfg["chunk_overlap"] = 200
	}
	return nil
}

func convertRetrieverTree(rootType string, params map[string]interface{}, subs []N8nSubNode) (N8nTree, error) {
	m := N8nNodeMappings[rootType]
	if m.Coverage == "unsupported" {
		return N8nTree{}, fmt.Errorf("n8n node %s is not supported: %s", rootType, m.Notes)
	}
	for _, sub := range subs {
		if sub.ConnectionType != "ai_vectorStore" {
			continue
		}
		res, err := ConvertN8nTree(sub.Type, sub.Parameters, sub.Children)
		if err != nil {
			return res, err
		}
		for k, v := range m.Fixed {
			res.Node.Config[k] = v
		}
		for path, ours := range m.Params {
			if v, ok := n8nParam(params, path); ok && v != nil {
				res.Node.Config[ours] = v
			}
		}
		return res, nil
	}
	return N8nTree{}, fmt.Errorf("%s has no ai_vectorStore sub-node", shortType(rootType))
}

func hasKind(list []N8nAttached, kind string) bool {
	for _, a := range list {
		if a.Kind == kind {
			return true
		}
	}
	return false
}
