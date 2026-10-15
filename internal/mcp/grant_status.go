package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/orgbridge"
	"github.com/monoes/mono-agent/internal/orgdesign"
	"github.com/monoes/mono-agent/internal/orggrant"
	"github.com/monoes/mono-agent/internal/profiledir"
	"github.com/monoes/mono-agent/internal/workflow"
)

// grantFinalReadTimeout bounds the last read of a run whose wait ended.
var grantFinalReadTimeout = 5 * time.Second

// waitForGrantRun polls the run until it is final or the wait ends. The
// wait ends at the tool's timeout, or earlier when ctx is cancelled: Serve
// cancels in-flight calls postEOFGrace after the client closes stdin. Either
// way the run is read once more on a fresh context, so a run that finished
// meanwhile still returns its output, and the note says how long the call
// actually waited and why it stopped. It used to report "still running
// after 600s" for a call a closed stdin cut off after 3 seconds (#244).
func waitForGrantRun(ctx context.Context, rt *runtime, id string, tool *orggrant.Tool) (map[string]interface{}, error) {
	start := time.Now()
	wctx, cancel := context.WithTimeout(ctx, time.Duration(tool.Timeout)*time.Second)
	defer cancel()
	for wctx.Err() == nil {
		view, final, err := executionView(wctx, rt, id, tool.MaxOutputBytes)
		if err != nil && wctx.Err() == nil {
			return nil, err
		}
		if err == nil && final {
			return view, nil
		}
		select {
		case <-wctx.Done():
		case <-time.After(grantPollInterval):
		}
	}
	// Measured before the final read, which is not waiting, and never more
	// than the tool's limit when the limit ended the wait: a slow machine
	// must not report a 1s limit as "waited 2s".
	waited := time.Since(start)
	if limit := time.Duration(tool.Timeout) * time.Second; ctx.Err() == nil && waited > limit {
		waited = limit
	}
	waited = waited.Round(time.Second)
	rctx, rcancel := context.WithTimeout(context.WithoutCancel(ctx), grantFinalReadTimeout)
	defer rcancel()
	view, final, err := executionView(rctx, rt, id, tool.MaxOutputBytes)
	if err != nil {
		return nil, err
	}
	if final {
		return view, nil
	}
	if ctx.Err() != nil {
		view["note"] = fmt.Sprintf("still running after %s; the call stopped waiting because the client closed the connection; check it with automation_status", waited)
	} else {
		view["note"] = fmt.Sprintf("still running after %s (the tool waits up to %ds); check it with automation_status", waited, tool.Timeout)
	}
	return view, nil
}

func orgLimitsFor(db *sql.DB, b *orggrant.Bundle) orgbridge.Limits {
	var lim orgbridge.Limits
	doc, err := orgdesign.Load(profiledir.Root(db, b.ProfileID), b.OrgName)
	if err != nil {
		return lim
	}
	if v, ok := doc.RunConfig["max_hops"]; ok {
		_ = json.Unmarshal(v, &lim.MaxHops)
	}
	if v, ok := doc.RunConfig["max_repeats"]; ok {
		_ = json.Unmarshal(v, &lim.MaxRepeats)
	}
	// run_config is in the org JSON, which any role whose fileWrite reaches
	// `.monomind/` can edit — the reason grants, endpoints and autonomy live
	// in the database — so clamp here as well as in Limits.withDefaults,
	// which bounds every Admit caller.
	if lim.MaxHops > orgbridge.MaxHopsCeiling {
		lim.MaxHops = orgbridge.MaxHopsCeiling
	}
	if lim.MaxRepeats > orgbridge.MaxRepeatsCeiling {
		lim.MaxRepeats = orgbridge.MaxRepeatsCeiling
	}
	return lim
}

// executionView renders an execution for a role: status, bounded redacted
// outputs of its last finished node, and a pending HIL item when it paused
// for review. final is true when the role should stop waiting.
func executionView(ctx context.Context, rt *runtime, id string, maxBytes int) (map[string]interface{}, bool, error) {
	exec, err := rt.store.GetExecution(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if exec == nil {
		return nil, false, fmt.Errorf("execution %s not found", id)
	}
	view := map[string]interface{}{"execution_id": id, "status": strings.ToLower(exec.Status)}
	switch exec.Status {
	case "SUCCESS":
		node, text := lastOutput(exec, "")
		view["node"] = node
		if maxBytes <= 0 {
			maxBytes = orggrant.DefaultMaxOutputBytes
		}
		if len(text) > maxBytes {
			view["output"] = text[:maxBytes]
			view["truncated"] = true
			view["output_bytes"] = len(text)
			view["note"] = "output truncated; read the rest with automation_output"
		} else {
			view["output"] = text
		}
		return view, true, nil
	case "FAILED", "CANCELLED":
		view["error"] = exec.ErrorMessage
		return view, true, nil
	case "WAITING":
		var hilID, nodeName string
		err := rt.db.DB.QueryRowContext(ctx, `SELECT id, node_name FROM hil_pending WHERE execution_id = ? AND status = 'pending' LIMIT 1`, id).Scan(&hilID, &nodeName)
		if err == nil {
			view["hil"] = map[string]interface{}{"id": hilID, "hint": fmt.Sprintf("the run paused at %q for review by the operator or the org's decider; you cannot approve it", nodeName)}
			return view, true, nil
		}
	}
	return view, false, nil
}

// lastOutput returns a node's output items (the last finished successful
// node when node is ""), redacted, as JSON text.
func lastOutput(exec *workflow.WorkflowExecution, node string) (string, string) {
	var pick *workflow.WorkflowExecutionNode
	for i := range exec.Nodes {
		n := &exec.Nodes[i]
		if n.Status != "SUCCESS" || len(n.OutputItems) == 0 && node == "" {
			continue
		}
		if node != "" {
			if n.NodeName == node {
				pick = n
			}
			continue
		}
		if pick == nil || (n.FinishedAt != nil && pick.FinishedAt != nil && !n.FinishedAt.Before(*pick.FinishedAt)) {
			pick = n
		}
	}
	if pick == nil {
		return "", "[]"
	}
	items := make([]interface{}, 0, len(pick.OutputItems))
	for _, it := range pick.OutputItems {
		items = append(items, workflow.RedactItemJSON(it).JSON)
	}
	b, _ := json.Marshal(items)
	return pick.NodeName, string(b)
}

// ownsExecution reports whether exec was started through one of the
// bundle's grants — a role reads only runs its own grants started.
func ownsExecution(ctx context.Context, rt *runtime, b *orggrant.Bundle, id string) bool {
	var n int
	_ = rt.db.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM org_bridge_calls c JOIN org_grants g ON g.id = c.grant_id
		 WHERE c.execution_id = ? AND g.profile_id = ? AND g.org_name = ? AND g.role_id = ?`,
		id, b.ProfileID, b.OrgName, b.RoleID).Scan(&n)
	return n > 0
}

func (s *Server) grantStatus(ctx context.Context, rt *runtime, b *orggrant.Bundle, args json.RawMessage) (interface{}, error) {
	var a struct {
		ExecutionID string `json:"execution_id"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	if a.ExecutionID == "" || !ownsExecution(ctx, rt, b, a.ExecutionID) {
		return nil, fmt.Errorf("%s: execution %q was not started by your automations", codeRefusedGrant, a.ExecutionID)
	}
	max := orggrant.DefaultMaxOutputBytes
	for _, g := range b.Grants {
		if t := g.Automation(); t != nil && t.MaxOutputBytes > 0 {
			max = t.MaxOutputBytes
		}
	}
	view, _, err := executionView(ctx, rt, a.ExecutionID, max)
	return view, err
}

func (s *Server) grantOutput(ctx context.Context, rt *runtime, b *orggrant.Bundle, args json.RawMessage) (interface{}, error) {
	var a struct {
		ExecutionID string `json:"execution_id"`
		Node        string `json:"node"`
		Offset      int    `json:"offset"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	if a.ExecutionID == "" || !ownsExecution(ctx, rt, b, a.ExecutionID) {
		return nil, fmt.Errorf("%s: execution %q was not started by your automations", codeRefusedGrant, a.ExecutionID)
	}
	exec, err := rt.store.GetExecution(ctx, a.ExecutionID)
	if err != nil || exec == nil {
		return nil, fmt.Errorf("execution %s not found", a.ExecutionID)
	}
	node, text := lastOutput(exec, a.Node)
	if a.Offset < 0 || a.Offset > len(text) {
		a.Offset = len(text)
	}
	end := a.Offset + orggrant.DefaultMaxOutputBytes
	if end > len(text) {
		end = len(text)
	}
	out := map[string]interface{}{"execution_id": a.ExecutionID, "node": node, "offset": a.Offset, "total_bytes": len(text), "chunk": text[a.Offset:end]}
	if end < len(text) {
		out["next_offset"] = end
	}
	return out, nil
}
