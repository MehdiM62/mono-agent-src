package chat

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
)

// NodeTypeInfo describes a node type available in the system.
type NodeTypeInfo struct {
	Type        string `json:"type"`
	Label       string `json:"label"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

// CanvasTools provides AI tool functions for manipulating workflow canvases.
type CanvasTools struct {
	db        *sql.DB
	nodeTypes []NodeTypeInfo

	mu        sync.RWMutex // guards profileID against concurrent stream/profile-switch access
	profileID string
}

// NewCanvasTools creates a CanvasTools backed by the given database and bound
// to profileID, the profile every call is scoped to. It is required: there is
// no default and no setter.
func NewCanvasTools(db *sql.DB, profileID string) *CanvasTools {
	return &CanvasTools{db: db, profileID: profileID}
}

// ProfileID returns the active profile id under the read lock. Callers that make
// several profile-scoped calls in one logical operation should snapshot it once.
func (ct *CanvasTools) ProfileID() string {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.profileID
}

// SetNodeTypes provides the list of available node types for list_available_nodes.
func (ct *CanvasTools) SetNodeTypes(types []NodeTypeInfo) {
	ct.nodeTypes = types
}

// checkWorkflowOwnership verifies that workflowID belongs to the active profile,
// preventing the AI chat tools from reading or mutating another profile's workflow.
// "general" and "draft" are placeholder IDs used before a real workflow exists
// (see systemPromptTemplate) and are always allowed through for read-only use;
// mutating tools additionally reject them via rejectPlaceholderWorkflowID.
func (ct *CanvasTools) checkWorkflowOwnership(workflowID string) error {
	if workflowID == "general" || workflowID == "draft" {
		return nil
	}
	var exists int
	err := ct.db.QueryRow(
		`SELECT 1 FROM workflows WHERE id = ? AND profile_id = ?`,
		workflowID, ct.ProfileID(),
	).Scan(&exists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("workflow %s not found", workflowID)
	}
	if err != nil {
		return fmt.Errorf("check workflow ownership: %w", err)
	}
	return nil
}

// rejectPlaceholderWorkflowID blocks mutating tools from operating on the
// "general"/"draft" placeholder IDs — those only exist before
// create_workflow, so any mutation against them would silently target
// nothing instead of prompting the model to create a workflow first.
func rejectPlaceholderWorkflowID(workflowID string) error {
	if workflowID == "general" || workflowID == "draft" {
		return fmt.Errorf("workflow_id %q is a placeholder — call create_workflow first", workflowID)
	}
	return nil
}

// CheckWorkflowOwnership is the exported pre-flight for external callers
// (e.g. the CLI chat command) that want to fail fast when the workflow
// doesn't exist or belongs to another profile — before spawning a backend.
func (ct *CanvasTools) CheckWorkflowOwnership(workflowID string) error {
	return ct.checkWorkflowOwnership(workflowID)
}

// ToolDefs returns the tool definitions the AI model can call.
func (ct *CanvasTools) ToolDefs() []ToolDef {
	return []ToolDef{
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_workflow_state",
				Description: "Get the current workflow nodes and connections",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"workflow_id": map[string]interface{}{"type": "string", "description": "The workflow ID"},
					},
					"required": []string{"workflow_id"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "create_workflow",
				Description: "Create a new workflow. Returns the new workflow_id. You MUST call this first before creating nodes when the current workflow_id is 'general' or 'draft'.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name":        map[string]interface{}{"type": "string", "description": "Name for the new workflow"},
						"description": map[string]interface{}{"type": "string", "description": "Optional description of what the workflow does"},
					},
					"required": []string{"name"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "create_nodes",
				Description: "Create one or more new nodes in a workflow",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"workflow_id": map[string]interface{}{"type": "string", "description": "The workflow ID"},
						"nodes": map[string]interface{}{
							"type":        "array",
							"description": "List of nodes to create",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"node_type":  map[string]interface{}{"type": "string", "description": "The node type identifier"},
									"name":       map[string]interface{}{"type": "string", "description": "Display name for the node"},
									"config":     map[string]interface{}{"type": "object", "description": "Node configuration JSON"},
									"position_x": map[string]interface{}{"type": "number", "description": "X position on canvas"},
									"position_y": map[string]interface{}{"type": "number", "description": "Y position on canvas"},
								},
								"required": []string{"node_type", "name"},
							},
						},
					},
					"required": []string{"workflow_id", "nodes"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "update_node_config",
				Description: "Update a node's configuration by merging new values into the existing config",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"workflow_id": map[string]interface{}{"type": "string", "description": "The workflow ID"},
						"node_id":     map[string]interface{}{"type": "string", "description": "The node ID to update"},
						"config":      map[string]interface{}{"type": "object", "description": "Config keys to merge into the existing config"},
					},
					"required": []string{"workflow_id", "node_id", "config"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "delete_nodes",
				Description: "Delete one or more nodes and their connections from a workflow",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"workflow_id": map[string]interface{}{"type": "string", "description": "The workflow ID"},
						"node_ids": map[string]interface{}{
							"type":        "array",
							"description": "List of node IDs to delete",
							"items":       map[string]interface{}{"type": "string"},
						},
					},
					"required": []string{"workflow_id", "node_ids"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "connect_nodes",
				Description: "Create a connection between two nodes",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"workflow_id":    map[string]interface{}{"type": "string", "description": "The workflow ID"},
						"source_node_id": map[string]interface{}{"type": "string", "description": "Source node ID"},
						"source_handle":  map[string]interface{}{"type": "string", "description": "Source output handle name"},
						"target_node_id": map[string]interface{}{"type": "string", "description": "Target node ID"},
						"target_handle":  map[string]interface{}{"type": "string", "description": "Target input handle name"},
					},
					"required": []string{"workflow_id", "source_node_id", "source_handle", "target_node_id", "target_handle"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "disconnect_nodes",
				Description: "Remove a connection between two nodes",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"workflow_id":    map[string]interface{}{"type": "string", "description": "The workflow ID"},
						"source_node_id": map[string]interface{}{"type": "string", "description": "Source node ID"},
						"target_node_id": map[string]interface{}{"type": "string", "description": "Target node ID"},
					},
					"required": []string{"workflow_id", "source_node_id", "target_node_id"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "list_available_nodes",
				Description: "List the available node types that can be added to a workflow",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"category": map[string]interface{}{"type": "string", "description": "Optional category filter (e.g. 'core', 'trigger', 'action')"},
					},
				},
			},
		},
	}
}

// Execute dispatches a tool call by name. args is the raw JSON arguments string.
// Returns a JSON result string.
func (ct *CanvasTools) Execute(name string, args string) (string, error) {
	if ct.ProfileID() == "" {
		return "", fmt.Errorf("%s: this tool set was built without a profile; every call must name the profile it acts on", name)
	}
	switch name {
	case "get_workflow_state":
		return ct.getWorkflowState(args)
	case "create_workflow":
		return ct.createWorkflow(args)
	case "create_nodes":
		return ct.createNodes(args)
	case "update_node_config":
		return ct.updateNodeConfig(args)
	case "delete_nodes":
		return ct.deleteNodes(args)
	case "connect_nodes":
		return ct.connectNodes(args)
	case "disconnect_nodes":
		return ct.disconnectNodes(args)
	case "list_available_nodes":
		return ct.listAvailableNodes(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// maxToolResultBytes caps a marshaled tool result before it enters the LLM
// conversation; oversized results (e.g. a full workflow state dump) are cut
// so both the live turn and the persisted history stay bounded.
const maxToolResultBytes = 32 * 1024

// truncatedResultMarker terminates any result cut at maxToolResultBytes.
const truncatedResultMarker = "...[truncated]"

func marshalJSON(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	if len(b) > maxToolResultBytes {
		b = append(b[:maxToolResultBytes:maxToolResultBytes], truncatedResultMarker...)
	}
	return string(b), nil
}
