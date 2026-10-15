package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monoes/mono-agent/internal/workflow"
)

// Workflow authoring over the workflow store, the operations of `workflow
// connect|disconnect|save` and `workflow node set|remove` (cmd/monoagentcli),
// which keep their logic in the command: these are the same steps over the
// same store APIs (GetWorkflow, SaveWorkflowNodes, SaveWorkflowConnections,
// SaveWorkflow), scoped to the call's profile.
func authoringTools() []tool {
	obj := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "object", "description": desc}
	}
	num := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "number", "description": desc}
	}
	return []tool{
		{
			name:        "workflow_connect",
			description: "Connect two nodes of a workflow, as `workflow connect`. from and to are \"nodeID\" or \"nodeID:handle\" (handle defaults to main). Returns the new connection.",
			schema: objSchema(map[string]interface{}{
				"workflow_id": strParam("The workflow ID"),
				"from":        strParam("Source node: nodeID[:handle]"),
				"to":          strParam("Target node: nodeID[:handle]"),
			}, "workflow_id", "from", "to"),
			annotations: map[string]bool{"readOnlyHint": false, "destructiveHint": false},
			mutating:    true,
			handler:     toolWorkflowConnect,
		},
		{
			name:        "workflow_disconnect",
			description: "Remove a connection from a workflow by its id (see workflow_get), as `workflow disconnect`.",
			schema: objSchema(map[string]interface{}{
				"workflow_id":   strParam("The workflow ID"),
				"connection_id": strParam("The connection ID"),
			}, "workflow_id", "connection_id"),
			annotations: map[string]bool{"readOnlyHint": false, "destructiveHint": true},
			mutating:    true,
			handler:     toolWorkflowDisconnect,
		},
		{
			name:        "workflow_node_update",
			description: "Update a node's name, config or canvas position, as `workflow node set`. config replaces the node's config unless merge is true, which sets only the keys given.",
			schema: objSchema(map[string]interface{}{
				"workflow_id": strParam("The workflow ID"),
				"node_id":     strParam("The node ID"),
				"name":        strParam("New display name (omit to leave unchanged)"),
				"config":      obj("New config object (omit to leave unchanged)"),
				"merge":       boolParam("Merge config into the existing config instead of replacing it (default false)"),
				"position_x":  num("Canvas X position (omit to leave unchanged)"),
				"position_y":  num("Canvas Y position (omit to leave unchanged)"),
			}, "workflow_id", "node_id"),
			annotations: map[string]bool{"readOnlyHint": false, "destructiveHint": true},
			mutating:    true,
			handler:     toolWorkflowNodeUpdate,
		},
		{
			name:        "workflow_node_delete",
			description: "Remove a node and every connection to or from it, as `workflow node remove`. Returns the removed node.",
			schema: objSchema(map[string]interface{}{
				"workflow_id": strParam("The workflow ID"),
				"node_id":     strParam("The node ID"),
			}, "workflow_id", "node_id"),
			annotations: map[string]bool{"readOnlyHint": false, "destructiveHint": true},
			mutating:    true,
			handler:     toolWorkflowNodeDelete,
		},
		{
			name: "workflow_save",
			description: "Save a whole workflow document in the shape workflow_get returns, as `workflow save`: without an id it creates a workflow, with one it replaces that workflow's name, description, nodes and connections. " +
				"Saving never changes whether a workflow is active. Returns the workflow without its nodes and connections.",
			schema: objSchema(map[string]interface{}{
				"workflow": obj("The workflow document: {id?, name, description?, nodes:[{id, node_type, name, config, position_x, position_y}], connections:[{id, source_node_id, source_handle, target_node_id, target_handle}]}"),
			}, "workflow"),
			annotations: map[string]bool{"readOnlyHint": false, "destructiveHint": true},
			mutating:    true,
			handler:     toolWorkflowSave,
		},
		{
			name:        "workflow_search",
			description: "Search bundled workflow templates and saved workflows by id, name, description or (saved ones) node type, as `workflow search`. Omit query to list everything.",
			schema: objSchema(map[string]interface{}{
				"query": strParam("Case-insensitive substring (optional)"),
			}),
			annotations: map[string]bool{"readOnlyHint": true, "idempotentHint": true},
			handler:     toolWorkflowSearch,
		},
		{
			name:        "workflow_inputs",
			description: "List the trigger fields a saved workflow's nodes read ($json.<field>), with examples and a ready-to-edit input skeleton for workflow_run, as `workflow inputs`.",
			schema: objSchema(map[string]interface{}{
				"id": strParam("Workflow ID"),
			}, "id"),
			annotations: map[string]bool{"readOnlyHint": true, "idempotentHint": true},
			handler:     toolWorkflowInputs,
		},
		{
			name:        "workflow_executions",
			description: "List recent executions of one workflow, or of every workflow of the profile when workflow_id is omitted, newest first, as `workflow executions`.",
			schema: objSchema(map[string]interface{}{
				"workflow_id": strParam("Workflow ID (omit for all workflows of the profile)"),
				"limit":       intParam("Max executions (default 20, 0 = all)"),
			}),
			annotations: map[string]bool{"readOnlyHint": true},
			handler:     toolWorkflowExecutions,
		},
		{
			name:        "workflow_template_list",
			global:      true, // reads no profile data
			description: "List the bundled workflow templates (ready-made workflows shipped with the binary): [{id, name, description, inputs}].",
			schema:      objSchema(nil),
			annotations: map[string]bool{"readOnlyHint": true, "idempotentHint": true},
			handler:     toolWorkflowTemplateList,
		},
		{
			name:        "workflow_template_show",
			global:      true, // reads no profile data
			description: "Show one bundled workflow template: its description, input keys and full definition.",
			schema: objSchema(map[string]interface{}{
				"id": strParam("Template ID from workflow_template_list"),
			}, "id"),
			annotations: map[string]bool{"readOnlyHint": true, "idempotentHint": true},
			handler:     toolWorkflowTemplateShow,
		},
		{
			name:        "workflow_template_instantiate",
			description: "Create a new, inactive, editable workflow from a bundled template, as `workflow templates use`. Returns the workflow; run it with workflow_run.",
			schema: objSchema(map[string]interface{}{
				"id": strParam("Template ID from workflow_template_list"),
			}, "id"),
			annotations: map[string]bool{"readOnlyHint": false, "destructiveHint": false},
			mutating:    true,
			handler:     toolWorkflowTemplateInstantiate,
		},
	}
}

// ownedWorkflow loads a workflow of the call's profile; another profile's
// is "not found", like every profile-scoped lookup here. A file-store copy
// knows no profile, so its SQLite row is asked too.
func ownedWorkflow(ctx context.Context, rt *runtime, id string) (*workflow.Workflow, error) {
	if id == "" {
		return nil, fmt.Errorf("workflow_id is required")
	}
	wf, err := rt.store.GetWorkflow(ctx, id)
	if err != nil || wf == nil || crossProfile(wf.ProfileID, profileOf(ctx)) {
		return nil, fmt.Errorf("workflow %q not found", id)
	}
	var owner string
	_ = rt.db.DB.QueryRowContext(ctx, `SELECT COALESCE(profile_id,'') FROM workflows WHERE id = ?`, id).Scan(&owner)
	if crossProfile(owner, profileOf(ctx)) {
		return nil, fmt.Errorf("workflow %q not found", id)
	}
	return wf, nil
}

// parseNodeHandle splits "nodeID[:handle]", the handle defaulting to main.
func parseNodeHandle(s string) (nodeID, handle string, err error) {
	if s == "" {
		return "", "", fmt.Errorf("value is required")
	}
	parts := strings.SplitN(s, ":", 2)
	nodeID, handle = parts[0], "main"
	if len(parts) == 2 && parts[1] != "" {
		handle = parts[1]
	}
	return nodeID, handle, nil
}

// saveConnections replaces wf's connections. Its nodes are written first: a
// workflow saved to the file store alone (the desktop editor) has no node
// rows in SQLite yet, and a connection row references two.
func saveConnections(ctx context.Context, rt *runtime, wf *workflow.Workflow, conns []workflow.WorkflowConnection) error {
	if err := rt.store.SaveWorkflowNodes(ctx, wf.ID, wf.Nodes); err != nil {
		return fmt.Errorf("save nodes: %w", err)
	}
	if err := rt.store.SaveWorkflowConnections(ctx, wf.ID, conns); err != nil {
		return fmt.Errorf("save connections: %w", err)
	}
	return nil
}

func hasNode(wf *workflow.Workflow, id string) bool {
	for _, n := range wf.Nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}

func toolWorkflowConnect(ctx context.Context, s *Server, args json.RawMessage) (interface{}, error) {
	var a struct {
		WorkflowID string `json:"workflow_id"`
		From       string `json:"from"`
		To         string `json:"to"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	srcID, srcHandle, err := parseNodeHandle(a.From)
	if err != nil {
		return nil, fmt.Errorf("from: %w", err)
	}
	dstID, dstHandle, err := parseNodeHandle(a.To)
	if err != nil {
		return nil, fmt.Errorf("to: %w", err)
	}
	rt, err := s.runtime()
	if err != nil {
		return nil, err
	}
	wf, err := ownedWorkflow(ctx, rt, a.WorkflowID)
	if err != nil {
		return nil, err
	}
	for _, id := range []string{srcID, dstID} {
		if !hasNode(wf, id) {
			return nil, fmt.Errorf("node %q not found in workflow %q", id, a.WorkflowID)
		}
	}
	conn := workflow.WorkflowConnection{
		ID:           uuid.New().String(),
		WorkflowID:   a.WorkflowID,
		SourceNodeID: srcID,
		SourceHandle: srcHandle,
		TargetNodeID: dstID,
		TargetHandle: dstHandle,
		Position:     len(wf.Connections),
	}
	if err := saveConnections(ctx, rt, wf, append(wf.Connections, conn)); err != nil {
		return nil, err
	}
	return conn, nil
}

func toolWorkflowDisconnect(ctx context.Context, s *Server, args json.RawMessage) (interface{}, error) {
	var a struct {
		WorkflowID   string `json:"workflow_id"`
		ConnectionID string `json:"connection_id"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	if a.ConnectionID == "" {
		return nil, fmt.Errorf("connection_id is required")
	}
	rt, err := s.runtime()
	if err != nil {
		return nil, err
	}
	wf, err := ownedWorkflow(ctx, rt, a.WorkflowID)
	if err != nil {
		return nil, err
	}
	kept := make([]workflow.WorkflowConnection, 0, len(wf.Connections))
	var removed *workflow.WorkflowConnection
	for i, c := range wf.Connections {
		if c.ID == a.ConnectionID {
			removed = &wf.Connections[i]
			continue
		}
		kept = append(kept, c)
	}
	if removed == nil {
		return nil, fmt.Errorf("connection %q not found in workflow %q", a.ConnectionID, a.WorkflowID)
	}
	if err := saveConnections(ctx, rt, wf, kept); err != nil {
		return nil, err
	}
	return map[string]interface{}{"removed": removed}, nil
}

func toolWorkflowNodeUpdate(ctx context.Context, s *Server, args json.RawMessage) (interface{}, error) {
	var a struct {
		WorkflowID string                 `json:"workflow_id"`
		NodeID     string                 `json:"node_id"`
		Name       *string                `json:"name"`
		Config     map[string]interface{} `json:"config"`
		Merge      bool                   `json:"merge"`
		PositionX  *float64               `json:"position_x"`
		PositionY  *float64               `json:"position_y"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	rt, err := s.runtime()
	if err != nil {
		return nil, err
	}
	wf, err := ownedWorkflow(ctx, rt, a.WorkflowID)
	if err != nil {
		return nil, err
	}
	for i := range wf.Nodes {
		n := &wf.Nodes[i]
		if n.ID != a.NodeID {
			continue
		}
		if a.Name != nil && *a.Name != "" {
			n.Name = *a.Name
		}
		if a.Config != nil {
			if a.Merge {
				if n.Config == nil {
					n.Config = map[string]interface{}{}
				}
				for k, v := range a.Config {
					n.Config[k] = v
				}
			} else {
				n.Config = a.Config
			}
			if err := n.MarshalConfig(); err != nil {
				return nil, fmt.Errorf("marshal config: %w", err)
			}
		}
		if a.PositionX != nil {
			n.PositionX = *a.PositionX
		}
		if a.PositionY != nil {
			n.PositionY = *a.PositionY
		}
		if err := rt.store.SaveWorkflowNodes(ctx, a.WorkflowID, wf.Nodes); err != nil {
			return nil, fmt.Errorf("save nodes: %w", err)
		}
		return *n, nil
	}
	return nil, fmt.Errorf("node %q not found in workflow %q", a.NodeID, a.WorkflowID)
}

func toolWorkflowNodeDelete(ctx context.Context, s *Server, args json.RawMessage) (interface{}, error) {
	var a struct {
		WorkflowID string `json:"workflow_id"`
		NodeID     string `json:"node_id"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	rt, err := s.runtime()
	if err != nil {
		return nil, err
	}
	wf, err := ownedWorkflow(ctx, rt, a.WorkflowID)
	if err != nil {
		return nil, err
	}
	nodes := make([]workflow.WorkflowNode, 0, len(wf.Nodes))
	var removed *workflow.WorkflowNode
	for i := range wf.Nodes {
		if wf.Nodes[i].ID == a.NodeID {
			removed = &wf.Nodes[i]
			continue
		}
		nodes = append(nodes, wf.Nodes[i])
	}
	if removed == nil {
		return nil, fmt.Errorf("node %q not found in workflow %q", a.NodeID, a.WorkflowID)
	}
	conns := make([]workflow.WorkflowConnection, 0, len(wf.Connections))
	for _, c := range wf.Connections {
		if c.SourceNodeID != a.NodeID && c.TargetNodeID != a.NodeID {
			conns = append(conns, c)
		}
	}
	wf.Nodes = nodes
	if err := saveConnections(ctx, rt, wf, conns); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"removed":             removed,
		"removed_connections": len(wf.Connections) - len(conns),
	}, nil
}

// toolWorkflowSave is `workflow save` (saveWorkflowDocument in
// cmd/monoagentcli/workflow_save.go): the stored activation, version and
// creation time of an existing workflow win over the document's, nodes
// without a schema get their type's, and the SQLite row is tagged with the
// saving profile.
func toolWorkflowSave(ctx context.Context, s *Server, args json.RawMessage) (interface{}, error) {
	var a struct {
		Workflow json.RawMessage `json:"workflow"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	if len(a.Workflow) == 0 || string(a.Workflow) == "null" {
		return nil, fmt.Errorf("workflow is required")
	}
	var doc workflow.Workflow
	if err := json.Unmarshal(a.Workflow, &doc); err != nil {
		return nil, fmt.Errorf("parse workflow document: %w", err)
	}
	rt, err := s.runtime()
	if err != nil {
		return nil, err
	}
	wf := &workflow.Workflow{ID: doc.ID, Name: doc.Name, Description: doc.Description, IsActive: doc.IsActive, ProfileID: profileOf(ctx)}
	if doc.ID != "" {
		existing, oerr := ownedWorkflow(ctx, rt, doc.ID)
		if oerr != nil {
			if other, _ := rt.store.GetWorkflow(ctx, doc.ID); other != nil {
				return nil, oerr
			}
		} else {
			wf.IsActive, wf.Version, wf.CreatedAt = existing.IsActive, existing.Version, existing.CreatedAt
		}
	}
	for _, n := range doc.Nodes {
		if n.Schema == nil {
			n.Schema, _ = workflow.LoadDefaultSchema(n.Type)
		}
		n.WorkflowID = ""
		wf.Nodes = append(wf.Nodes, n)
	}
	for _, c := range doc.Connections {
		c.WorkflowID = ""
		wf.Connections = append(wf.Connections, c)
	}
	if err := rt.store.SaveWorkflow(ctx, wf); err != nil {
		return nil, fmt.Errorf("save workflow: %w", err)
	}
	_, _ = rt.db.DB.ExecContext(ctx, `INSERT OR IGNORE INTO workflows
		(id, name, description, is_active, version, profile_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		wf.ID, wf.Name, wf.Description, wf.IsActive, wf.Version, profileOf(ctx),
		wf.CreatedAt.UTC().Format(time.RFC3339), wf.UpdatedAt.UTC().Format(time.RFC3339))
	_, _ = rt.db.DB.ExecContext(ctx, `UPDATE workflows SET profile_id = ? WHERE id = ?`, profileOf(ctx), wf.ID)
	summary := *wf
	summary.Nodes, summary.Connections = nil, nil
	return summary, nil
}
