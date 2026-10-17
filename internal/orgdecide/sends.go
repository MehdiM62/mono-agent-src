package orgdecide

import (
	"context"
	"database/sql"
	"strings"

	"github.com/monoes/mono-agent/internal/commsync"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/orggrant"
	"github.com/monoes/mono-agent/internal/workflow"
)

// A message to a person is the user's to approve (#831 round 7): an item
// whose approval lets one go out goes to a person at every level and tier,
// whatever decides the org's other items (a rule, the model, Jev, a boss or
// parent role).

// sendsMaxDepth bounds how deep workflowSends follows called workflows; a
// call past it counts as sending.
const sendsMaxDepth = 8

// ItemSends reports whether approving it, an item of org in profileID, lets
// a message to a person go out: a Human-in-Loop item whose approval
// releases one (commsync.OutboundHIL), or a call of a granted automation
// whose workflow, as load reads it now, may send (workflowSends). What can't
// be told counts as sending.
func ItemSends(ctx context.Context, db *sql.DB, profileID, org string, it Item, load orggrant.WorkflowLoader) bool {
	switch {
	case it.Kind == KindHIL:
		var nc string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(node_config, '') FROM hil_pending WHERE id = ?`, it.Ref).Scan(&nc); err != nil {
			return true
		}
		return commsync.OutboundHIL(nc)
	case it.Kind == KindApproval && strings.HasPrefix(it.Class, "grant:"):
		return grantSends(ctx, db, profileID, org, strings.TrimPrefix(it.Class, "grant:"), load)
	}
	return false
}

// grantSends reports whether a call of org's automation alias may send: its
// grant's workflow does (workflowSends), or there is no grant or workflow to
// tell by.
func grantSends(ctx context.Context, db *sql.DB, profileID, org, alias string, load orggrant.WorkflowLoader) bool {
	grants, err := orggrant.NewStore(db).ListGrants(ctx, profileID, org, "")
	if err != nil || load == nil {
		return true
	}
	for _, g := range grants {
		if t := g.Automation(); t != nil && t.Alias == alias {
			wf, err := load(ctx, t.WorkflowID)
			if err != nil || wf == nil {
				return true
			}
			return workflowSends(ctx, load, wf, map[string]bool{wf.ID: true}, 0)
		}
	}
	return true
}

// workflowSends reports whether running wf may send a message to a person:
// one of its nodes (disabled ones too), or of a workflow it calls
// (core.execute_workflow, by id), is a send as an agent's sends are
// classified (hilsend.MaySend). A call it can't follow counts as sending.
func workflowSends(ctx context.Context, load orggrant.WorkflowLoader, wf *workflow.Workflow, called map[string]bool, depth int) bool {
	for _, n := range wf.Nodes {
		_ = n.ParseConfig()
		if hilsend.MaySend(n.Type, n.Config) {
			return true
		}
		if n.Type != "core.execute_workflow" {
			continue
		}
		ref, _ := n.Config["workflowId"].(string)
		ref = strings.TrimSpace(ref)
		if ref == "" || strings.Contains(ref, "{{") || strings.HasPrefix(ref, "=") || depth+1 > sendsMaxDepth {
			return true
		}
		if called[ref] {
			continue
		}
		sub, err := load(ctx, ref)
		if err != nil || sub == nil {
			return true
		}
		called[ref] = true
		if workflowSends(ctx, load, sub, called, depth+1) {
			return true
		}
	}
	return false
}
