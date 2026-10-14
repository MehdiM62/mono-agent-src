package apipkg

import (
	"context"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

// dispatchAndWait runs a wait op (n8n's dispatchAndWait): the request is
// sent once, for the first item, with hook.url set to the execution's
// resume URL, and the node pauses until that URL is called; the call's body
// is the output (workflow.WaitForCall). The limit is read before anything
// is sent, so a bad limit never leaves a dispatched job with nothing
// waiting for it.
func (r *opRun) dispatchAndWait(ctx context.Context, input workflow.NodeInput) ([]workflow.NodeOutput, error) {
	spec := workflow.WaitReplySpec{HTTPMethod: r.op.Wait.Method}
	if r.op.Wait.Limit != "" {
		if on, _ := lookupPath(r.params, strings.Split(r.op.Wait.Limit, ".")).(map[string]any); len(on) > 0 {
			limit, err := workflow.ParseReplyLimit(on, time.Now())
			if err != nil {
				return nil, r.errf("wait limit: %w", err)
			}
			spec.Limit = limit
		}
	}
	return workflow.WaitForCall(ctx, input, spec, func(ctx context.Context, reply workflow.WaitReply) error {
		r.scope.Hook = map[string]any{"url": reply.URL}
		_, err := r.execute(ctx)
		return err
	})
}
