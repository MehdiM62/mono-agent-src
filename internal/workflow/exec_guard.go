package workflow

import (
	"context"
	"sync/atomic"
)

// ExecGuard sees every node before it runs. When it reports handled, its
// outputs and error stand in for the node's own and the node does not run.
// Outbound sends use it to wait for a person's approval (internal/hilsend).
type ExecGuard func(ctx context.Context, nodeType string, in NodeInput, config map[string]interface{}) (out []NodeOutput, handled bool, err error)

var execGuard atomic.Pointer[ExecGuard]

// SetExecGuard installs g for the process; nil removes it.
func SetExecGuard(g ExecGuard) {
	if g == nil {
		execGuard.Store(nil)
		return
	}
	execGuard.Store(&g)
}

// RunExecGuard runs the installed guard, if any.
func RunExecGuard(ctx context.Context, nodeType string, in NodeInput, config map[string]interface{}) ([]NodeOutput, bool, error) {
	if g := execGuard.Load(); g != nil {
		return (*g)(ctx, nodeType, in, config)
	}
	return nil, false, nil
}
