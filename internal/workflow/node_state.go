package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// ErrNodeStateUnavailable is returned by SetNodeState when the context
// carries no state slot — the node is running outside RunExecution (a unit
// test, a single-node preview), so nothing would persist what it saves.
var ErrNodeStateUnavailable = errors.New("workflow: node state is not available outside an execution")

// nodeStateSlot holds one node's private state for the duration of one
// node run. RunExecution seeds it from the execution's saved state and
// stores whatever is left in it afterwards, so the state survives a pause
// (WAITING) and a process restart, and is visible to the node's next run in
// the same execution — a loop's next iteration or a resumed wait.
type nodeStateSlot struct {
	mu  sync.Mutex
	raw json.RawMessage
}

type nodeStateKey struct{}

// WithNodeState returns a context carrying a fresh state slot that starts
// with state (nil for none). RunExecution calls it for every node run; node
// tests call it to simulate a run and a resume.
func WithNodeState(ctx context.Context, state json.RawMessage) context.Context {
	return context.WithValue(ctx, nodeStateKey{}, &nodeStateSlot{raw: state})
}

// NodeStateFromContext returns the running node's saved state as JSON, or
// nil when it has none (or the context carries no slot).
func NodeStateFromContext(ctx context.Context) json.RawMessage {
	slot, _ := ctx.Value(nodeStateKey{}).(*nodeStateSlot)
	if slot == nil {
		return nil
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	return slot.raw
}

// SetNodeState saves v (JSON-encoded) as the running node's state for its
// next run in this execution; nil clears it. A node that finishes its work
// should clear its state so the next run (e.g. the next loop iteration)
// starts fresh.
func SetNodeState(ctx context.Context, v interface{}) error {
	slot, _ := ctx.Value(nodeStateKey{}).(*nodeStateSlot)
	if slot == nil {
		return ErrNodeStateUnavailable
	}
	var raw json.RawMessage
	if v != nil {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		raw = b
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	slot.raw = raw
	return nil
}
