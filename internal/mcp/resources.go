package mcp

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/accountdoor"
	"github.com/monoes/mono-agent/internal/noderegistry"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Resources (MCP resources/list, resources/templates/list, resources/read):
// the reference topics of the docs tool as monoagent://ref/<topic>, and the
// node types' config schemas, the node_schema tool's documents, as
// monoagent://node/<type>. They are what the docs and node_schema tools
// answer, offered for hosts that attach context by URI.
const (
	refURIPrefix  = "monoagent://ref/"
	nodeURIPrefix = "monoagent://node/"
)

// resourceNotFound is the JSON-RPC error code MCP gives an unknown resource.
const resourceNotFound = -32002

// servesResources reports whether this server serves resources. Grant mode
// serves an org role's automations and nothing else, and a --tasks-only
// server the task board's tools and nothing else: neither gains them. A
// --api-only server serves the one topic about its tools, api-tools.
func (s *Server) servesResources() bool {
	return s.opts.Grant == "" && !s.opts.TasksOnly
}

// servedRefTopics are the reference topics this server offers, sorted.
func (s *Server) servedRefTopics() []string {
	if s.opts.APIOnly {
		return []string{apiToolsTopic}
	}
	topics := make([]string, 0, len(docsTopics))
	for t := range docsTopics {
		topics = append(topics, t)
	}
	sort.Strings(topics)
	return topics
}

// nodeSchemaTypes are the node types with a schema document: every
// registered type and every embedded schema (trigger types have one and no
// executor), sorted, as node_schema accepts them.
func nodeSchemaTypes() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range append(noderegistry.Build(nil).Types(), workflow.ListEmbeddedSchemas()...) {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Server) handleResources(ctx context.Context, req rpcRequest) *rpcResponse {
	switch req.Method {
	case "resources/list":
		var list []map[string]interface{}
		for _, t := range s.servedRefTopics() {
			list = append(list, map[string]interface{}{
				"uri": refURIPrefix + t, "name": "ref " + t, "mimeType": "text/plain",
				"description": "monoagentcli reference: " + t,
			})
		}
		if !s.opts.APIOnly {
			for _, t := range nodeSchemaTypes() {
				list = append(list, map[string]interface{}{
					"uri": nodeURIPrefix + t, "name": "node " + t, "mimeType": "application/json",
				})
			}
		}
		return s.result(req.ID, map[string]interface{}{"resources": list})

	case "resources/templates/list":
		templates := []map[string]interface{}{{
			"uriTemplate": refURIPrefix + "{topic}", "name": "ref topic", "mimeType": "text/plain",
			"description": "A monoagentcli reference topic (the docs tool's topics)",
		}}
		if !s.opts.APIOnly {
			templates = append(templates, map[string]interface{}{
				"uriTemplate": nodeURIPrefix + "{type}", "name": "node schema", "mimeType": "application/json",
				"description": "The config schema of a node type, as node_schema returns it",
			})
		}
		return s.result(req.ID, map[string]interface{}{"resourceTemplates": templates})
	}

	// resources/read is work, refused without a signed-in account as tools/call is.
	if err := account.Require(ctx); err != nil {
		return s.rpcErr(req.ID, -32001, accountdoor.Message)
	}
	var p struct {
		URI string `json:"uri"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return s.rpcErr(req.ID, -32602, "invalid resources/read params: "+err.Error())
		}
	}
	text, mime, ok := s.readResource(p.URI)
	if !ok {
		return s.rpcErr(req.ID, resourceNotFound, "resource not found: "+p.URI)
	}
	return s.result(req.ID, map[string]interface{}{"contents": []map[string]interface{}{{
		"uri": p.URI, "mimeType": mime, "text": text,
	}}})
}

// readResource is the content of uri, if this server serves it.
func (s *Server) readResource(uri string) (text, mime string, ok bool) {
	switch {
	case strings.HasPrefix(uri, refURIPrefix):
		topic := strings.TrimPrefix(uri, refURIPrefix)
		for _, t := range s.servedRefTopics() {
			if t == topic {
				return docsTopics[topic], "text/plain", true
			}
		}
	case strings.HasPrefix(uri, nodeURIPrefix) && !s.opts.APIOnly:
		nodeType := strings.TrimPrefix(uri, nodeURIPrefix)
		types := nodeSchemaTypes()
		i := sort.SearchStrings(types, nodeType)
		if i >= len(types) || types[i] != nodeType {
			return "", "", false
		}
		schema, err := workflow.LoadDefaultSchema(nodeType)
		if err != nil {
			return "", "", false
		}
		b, err := json.MarshalIndent(map[string]interface{}{"type": nodeType, "schema": schema}, "", "  ")
		if err != nil {
			return "", "", false
		}
		return string(b), "application/json", true
	}
	return "", "", false
}

func (s *Server) rpcErr(id json.RawMessage, code int, msg string) *rpcResponse {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}
