package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

// notifyFunc writes one JSON-RPC notification to the client.
type notifyFunc func(method string, params interface{})

type notifierKey struct{}
type progressKey struct{}

// withNotifier gives handlers a way to write notifications between responses.
func withNotifier(ctx context.Context, notify notifyFunc) context.Context {
	return context.WithValue(ctx, notifierKey{}, notify)
}

// progressReporter sends notifications/progress for one tools/call whose
// request carried params._meta.progressToken (MCP 2025-06-18): progress
// only ever increases, and a report that would not is dropped.
type progressReporter struct {
	token  json.RawMessage
	notify notifyFunc

	mu   sync.Mutex
	last float64
	sent bool
}

// withProgress attaches a reporter when meta has a progressToken and the
// server can write notifications; otherwise ctx is returned as it is.
func withProgress(ctx context.Context, meta json.RawMessage) context.Context {
	if len(meta) == 0 {
		return ctx
	}
	var m struct {
		ProgressToken json.RawMessage `json:"progressToken"`
	}
	if json.Unmarshal(meta, &m) != nil || len(m.ProgressToken) == 0 || string(m.ProgressToken) == "null" {
		return ctx
	}
	notify, _ := ctx.Value(notifierKey{}).(notifyFunc)
	if notify == nil {
		return ctx
	}
	return context.WithValue(ctx, progressKey{}, &progressReporter{token: m.ProgressToken, notify: notify})
}

// progressFrom is the call's reporter, or nil when the client asked for none.
func progressFrom(ctx context.Context) *progressReporter {
	p, _ := ctx.Value(progressKey{}).(*progressReporter)
	return p
}

// report sends one progress notification; total <= 0 leaves total out.
// Safe on a nil reporter.
func (p *progressReporter) report(progress, total float64, message string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.sent && progress <= p.last {
		p.mu.Unlock()
		return
	}
	p.last, p.sent = progress, true
	p.mu.Unlock()
	params := map[string]interface{}{"progressToken": p.token, "progress": progress}
	if total > 0 {
		params["total"] = total
	}
	if message != "" {
		params["message"] = message
	}
	p.notify("notifications/progress", params)
}

// progressInterval is how often a workflow_run that reports progress looks
// at its execution's nodes.
const progressInterval = time.Second

// waitWithProgress is waitForExecution for a call that asked for progress:
// while it waits it reports how many of the workflow's nodes have finished,
// once a second at most and only when that number grows, then the end.
func waitWithProgress(ctx context.Context, rt *runtime, executionID, workflowID string, timeout time.Duration, p *progressReporter) *workflow.WorkflowExecution {
	total := 0
	if wf, err := rt.store.GetWorkflow(ctx, workflowID); err == nil && wf != nil {
		total = len(wf.Nodes)
	}
	p.report(0, float64(total), "queued")
	done := make(chan *workflow.WorkflowExecution, 1)
	go func() { done <- waitForExecution(ctx, rt, executionID, timeout) }()
	ticker := time.NewTicker(progressInterval)
	defer ticker.Stop()
	for {
		select {
		case exec := <-done:
			finished := float64(finishedNodes(exec))
			if total > 0 && finished < float64(total) && exec.Status == "SUCCESS" {
				finished = float64(total) // nodes a branch skipped never get a row
			}
			p.report(finished, float64(total), "execution "+strings.ToLower(exec.Status))
			return exec
		case <-ticker.C:
			n, status := nodeProgress(ctx, rt, executionID)
			msg := fmt.Sprintf("%d node(s) finished", n)
			if total > 0 {
				msg = fmt.Sprintf("%d of %d node(s) finished", n, total)
			}
			if status != "" {
				msg += ", execution " + strings.ToLower(status)
			}
			p.report(float64(n), float64(total), msg)
		}
	}
}

// nodeProgress counts an execution's finished node rows, and reads its status.
func nodeProgress(ctx context.Context, rt *runtime, executionID string) (int, string) {
	var n int
	_ = rt.db.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workflow_execution_nodes WHERE execution_id = ? AND status IN ('SUCCESS','FAILED','SKIPPED')`,
		executionID).Scan(&n)
	status, _ := rt.store.GetExecutionStatus(ctx, executionID)
	return n, status
}

// finishedNodes counts the nodes of a loaded execution that ran to an end.
func finishedNodes(exec *workflow.WorkflowExecution) int {
	n := 0
	for _, en := range exec.Nodes {
		switch en.Status {
		case "SUCCESS", "FAILED", "SKIPPED":
			n++
		}
	}
	return n
}
