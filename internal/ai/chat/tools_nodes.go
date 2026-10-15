package chat

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monoes/mono-agent/internal/workflow"
)

// --- tool implementations ---

type getWorkflowStateArgs struct {
	WorkflowID string `json:"workflow_id"`
}

type nodeRow struct {
	ID        string  `json:"id"`
	NodeType  string  `json:"node_type"`
	Name      string  `json:"name"`
	Config    any     `json:"config"`
	PositionX float64 `json:"position_x"`
	PositionY float64 `json:"position_y"`
	Disabled  bool    `json:"disabled"`
}

type connectionRow struct {
	ID           string `json:"id"`
	SourceNodeID string `json:"source_node_id"`
	SourceHandle string `json:"source_handle"`
	TargetNodeID string `json:"target_node_id"`
	TargetHandle string `json:"target_handle"`
	Position     int    `json:"position"`
}

func (ct *CanvasTools) getWorkflowState(args string) (string, error) {
	var a getWorkflowStateArgs
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if err := ct.checkWorkflowOwnership(a.WorkflowID); err != nil {
		return "", err
	}

	// Query nodes
	rows, err := ct.db.Query(
		`SELECT id, node_type, name, config, position_x, position_y, disabled
		 FROM workflow_nodes WHERE workflow_id = ?`, a.WorkflowID)
	if err != nil {
		return "", fmt.Errorf("query nodes: %w", err)
	}
	defer rows.Close()

	nodes := make([]nodeRow, 0)
	for rows.Next() {
		var n nodeRow
		var configStr string
		if err := rows.Scan(&n.ID, &n.NodeType, &n.Name, &configStr, &n.PositionX, &n.PositionY, &n.Disabled); err != nil {
			return "", fmt.Errorf("scan node: %w", err)
		}
		if configStr != "" {
			var cfg interface{}
			if err := json.Unmarshal([]byte(configStr), &cfg); err == nil {
				n.Config = redactConfigSecrets(cfg)
			} else {
				n.Config = configStr
			}
		} else {
			n.Config = map[string]interface{}{}
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate nodes: %w", err)
	}

	// Query connections
	connRows, err := ct.db.Query(
		`SELECT id, source_node_id, source_handle, target_node_id, target_handle, position
		 FROM workflow_connections WHERE workflow_id = ?`, a.WorkflowID)
	if err != nil {
		return "", fmt.Errorf("query connections: %w", err)
	}
	defer connRows.Close()

	connections := make([]connectionRow, 0)
	for connRows.Next() {
		var c connectionRow
		if err := connRows.Scan(&c.ID, &c.SourceNodeID, &c.SourceHandle, &c.TargetNodeID, &c.TargetHandle, &c.Position); err != nil {
			return "", fmt.Errorf("scan connection: %w", err)
		}
		connections = append(connections, c)
	}
	if err := connRows.Err(); err != nil {
		return "", fmt.Errorf("iterate connections: %w", err)
	}

	result := map[string]interface{}{
		"workflow_id": a.WorkflowID,
		"nodes":       nodes,
		"connections": connections,
	}
	return marshalJSON(result)
}

type createWorkflowArgs struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// redactConfigSecrets masks credential values in a parsed node config
// (api_key/token/secret/password/authorization/cookie/…) before the state
// crosses the LLM boundary, reusing the workflow store's key-name
// redactor so both stay in sync. Non-map configs pass through untouched.
func redactConfigSecrets(cfg interface{}) interface{} {
	m, ok := cfg.(map[string]interface{})
	if !ok {
		return cfg
	}
	return workflow.RedactItems([]map[string]interface{}{m})[0]
}

func (ct *CanvasTools) createWorkflow(args string) (string, error) {
	var a createWorkflowArgs
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	id := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := ct.db.Exec(
		`INSERT INTO workflows (id, name, description, is_active, version, profile_id, created_at, updated_at)
		 VALUES (?, ?, ?, 0, 1, ?, ?, ?)`,
		id, a.Name, a.Description, ct.ProfileID(), now, now); err != nil {
		return "", fmt.Errorf("insert workflow: %w", err)
	}

	result := map[string]interface{}{
		"workflow_id": id,
		"name":        a.Name,
	}
	return marshalJSON(result)
}

type createNodesArgs struct {
	WorkflowID string           `json:"workflow_id"`
	Nodes      []createNodeSpec `json:"nodes"`
}

type createNodeSpec struct {
	NodeType string `json:"node_type"`
	// Type is a tolerated alias for NodeType. Measured live against both
	// Claude and Codex building the same workflow: both independently sent
	// "type" instead of "node_type" — the same "mistake" from two unrelated
	// models is a schema-ambiguity problem, not a model-competence one.
	// Every earlier version of this tool silently accepted the resulting
	// empty NodeType and persisted a broken, non-executable node with no
	// error — production data corruption with a 100% success-looking
	// response. Accepting the alias fixes the common case outright; the
	// validation below still catches anything neither key covers.
	Type      string                 `json:"type"`
	Name      string                 `json:"name"`
	Config    map[string]interface{} `json:"config"`
	PositionX float64                `json:"position_x"`
	PositionY float64                `json:"position_y"`
}

func (ct *CanvasTools) createNodes(args string) (string, error) {
	var a createNodesArgs
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if err := rejectPlaceholderWorkflowID(a.WorkflowID); err != nil {
		return "", err
	}
	if err := ct.checkWorkflowOwnership(a.WorkflowID); err != nil {
		return "", err
	}
	knownTypes := make(map[string]bool, len(ct.nodeTypes))
	for _, t := range ct.nodeTypes {
		knownTypes[t.Type] = true
	}

	// Resolve and validate every node before writing anything — a batch
	// either fully succeeds or fails clearly, never leaving some nodes
	// created and others silently skipped/broken.
	resolvedTypes := make([]string, len(a.Nodes))
	resolvedNames := make([]string, len(a.Nodes))
	for i, n := range a.Nodes {
		nodeType := n.NodeType
		if nodeType == "" {
			nodeType = n.Type
		}
		if nodeType == "" {
			return "", fmt.Errorf("nodes[%d]: node_type is required — call list_available_nodes for valid values", i)
		}
		// Trigger nodes (trigger.manual/schedule/webhook) are a separate
		// subsystem from the generic executor registry list_available_nodes
		// draws from — internal/workflow/trigger_manager.go and validator.go
		// both recognize them by this same "trigger." prefix rather than
		// registry membership. Registry-checking them here would reject a
		// node type that real workflows in this DB already use — verified
		// live: an existing "Gemini — Generate One Image" workflow has a
		// trigger.manual node, but registryNodeTypes()/list_available_nodes
		// return no trigger.* entries at all.
		isTrigger := strings.HasPrefix(nodeType, "trigger.")
		if len(knownTypes) > 0 && !knownTypes[nodeType] && !isTrigger {
			return "", fmt.Errorf("nodes[%d]: unknown node_type %q — call list_available_nodes for valid values", i, nodeType)
		}
		resolvedTypes[i] = nodeType
		name := n.Name
		if name == "" {
			name = nodeType
		}
		resolvedNames[i] = name
	}

	now := time.Now().UTC()
	createdIDs := make([]string, 0, len(a.Nodes))

	tx, err := ct.db.Begin()
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT INTO workflow_nodes (id, workflow_id, node_type, name, config, position_x, position_y, disabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`)
	if err != nil {
		return "", fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for i, n := range a.Nodes {
		id := uuid.New().String()
		configJSON := "{}"
		if n.Config != nil {
			b, err := json.Marshal(n.Config)
			if err != nil {
				return "", fmt.Errorf("marshal config: %w", err)
			}
			configJSON = string(b)
		}
		if _, err := stmt.Exec(id, a.WorkflowID, resolvedTypes[i], resolvedNames[i], configJSON, n.PositionX, n.PositionY, now, now); err != nil {
			return "", fmt.Errorf("insert node: %w", err)
		}
		createdIDs = append(createdIDs, id)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	result := map[string]interface{}{
		"created_node_ids": createdIDs,
	}
	return marshalJSON(result)
}

type updateNodeConfigArgs struct {
	WorkflowID string                 `json:"workflow_id"`
	NodeID     string                 `json:"node_id"`
	Config     map[string]interface{} `json:"config"`
}

func (ct *CanvasTools) updateNodeConfig(args string) (string, error) {
	var a updateNodeConfigArgs
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if err := rejectPlaceholderWorkflowID(a.WorkflowID); err != nil {
		return "", err
	}
	if err := ct.checkWorkflowOwnership(a.WorkflowID); err != nil {
		return "", err
	}

	// Read existing config
	var existingConfig string
	err := ct.db.QueryRow(
		`SELECT config FROM workflow_nodes WHERE id = ? AND workflow_id = ?`,
		a.NodeID, a.WorkflowID).Scan(&existingConfig)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("node %s not found in workflow %s", a.NodeID, a.WorkflowID)
	}
	if err != nil {
		return "", fmt.Errorf("query node: %w", err)
	}

	// Parse existing config
	existing := make(map[string]interface{})
	if existingConfig != "" {
		if err := json.Unmarshal([]byte(existingConfig), &existing); err != nil {
			return "", fmt.Errorf("parse existing config: %w", err)
		}
	}

	// Merge new keys into existing
	for k, v := range a.Config {
		existing[k] = v
	}

	merged, err := json.Marshal(existing)
	if err != nil {
		return "", fmt.Errorf("marshal merged config: %w", err)
	}

	now := time.Now().UTC()
	if _, err := ct.db.Exec(
		`UPDATE workflow_nodes SET config = ?, updated_at = ? WHERE id = ? AND workflow_id = ?`,
		string(merged), now, a.NodeID, a.WorkflowID); err != nil {
		return "", fmt.Errorf("update node: %w", err)
	}

	result := map[string]interface{}{
		"node_id": a.NodeID,
		"config":  redactConfigSecrets(existing),
	}
	return marshalJSON(result)
}
