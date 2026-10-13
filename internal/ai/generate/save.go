package generate

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Draft is wf readied for saving as a NEW inactive workflow: no id (the
// store assigns one, so an id the model wrote can never replace an existing
// workflow), and fresh node and connection ids, since the short ids a model
// writes (n1, c1) would collide with another workflow's rows in SQLite and
// leave this one unmirrored. Connections follow their nodes' new ids.
func Draft(wf *workflow.Workflow) workflow.Workflow {
	out := workflow.Workflow{Name: wf.Name, Description: wf.Description, IsActive: false}
	ids := make(map[string]string, len(wf.Nodes))
	for _, n := range wf.Nodes {
		nid := uuid.New().String()
		ids[n.ID] = nid
		n.ID, n.WorkflowID = nid, ""
		out.Nodes = append(out.Nodes, n)
	}
	for _, c := range wf.Connections {
		c.ID, c.WorkflowID = uuid.New().String(), ""
		c.SourceNodeID, c.TargetNodeID = ids[c.SourceNodeID], ids[c.TargetNodeID]
		out.Connections = append(out.Connections, c)
	}
	return out
}

// TagProfile writes the SQLite metadata row `workflow save` writes after
// saving: the file is canonical, but executions and profile scoping need a
// row tagged with the saving profile.
func TagProfile(ctx context.Context, db *sql.DB, wf *workflow.Workflow, profileID string) {
	if db == nil || wf == nil {
		return
	}
	_, _ = db.ExecContext(ctx, `INSERT OR IGNORE INTO workflows
		(id, name, description, is_active, version, profile_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		wf.ID, wf.Name, wf.Description, wf.IsActive, wf.Version, profileID,
		wf.CreatedAt.UTC().Format(time.RFC3339), wf.UpdatedAt.UTC().Format(time.RFC3339))
	_, _ = db.ExecContext(ctx, `UPDATE workflows SET profile_id = ? WHERE id = ?`, profileID, wf.ID)
}

// SaveDraft saves wf as a new inactive workflow of profileID the way
// `workflow save` saves a document without an id: nodes without a schema get
// their type's default one, the store writes the file and mirrors it, and
// the metadata row is tagged with the profile. The MCP tool uses it; the
// CLI command calls `workflow save`'s own function.
func SaveDraft(ctx context.Context, store *workflow.HybridWorkflowStore, db *sql.DB, profileID string, wf *workflow.Workflow) (*workflow.Workflow, error) {
	draft := Draft(wf)
	draft.ProfileID = profileID
	for i := range draft.Nodes {
		if draft.Nodes[i].Schema == nil {
			draft.Nodes[i].Schema, _ = workflow.LoadDefaultSchema(draft.Nodes[i].Type)
		}
	}
	if err := store.SaveWorkflow(ctx, &draft); err != nil {
		return nil, fmt.Errorf("save workflow: %w", err)
	}
	TagProfile(ctx, db, &draft, profileID)
	return &draft, nil
}
