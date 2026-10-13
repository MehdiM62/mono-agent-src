package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// ErrNodeTimeout marks an attempt stopped by the node's opt-in node_timeout.
// It is retried like any transient error under the node's retry_policy.
var ErrNodeTimeout = errors.New("workflow: node timed out")

// nodeTimeoutError is an attempt's error after its node_timeout expired. It
// matches ErrNodeTimeout and still unwraps to what the node returned.
type nodeTimeoutError struct {
	after time.Duration
	err   error
}

func (e *nodeTimeoutError) Error() string {
	return fmt.Sprintf("node timed out after %s: %v", e.after, e.err)
}
func (e *nodeTimeoutError) Unwrap() error        { return e.err }
func (e *nodeTimeoutError) Is(target error) bool { return target == ErrNodeTimeout }

// PartialItemsError is returned by a node that processed some input items and
// failed others. Succeeded holds the outputs of the items that worked;
// FailedIndexes index the input.Items the node was given, with Errs[i] the
// error for FailedIndexes[i].
//
// executeWithRetry retries only the failed items whose error is retryable,
// merging successes across attempts. When items still fail after the last
// attempt it returns the merged successes both as its outputs and in
// Succeeded, with FailedIndexes then indexing the node's original input.
type PartialItemsError struct {
	Succeeded     []NodeOutput
	FailedIndexes []int
	Errs          []error
}

func (e *PartialItemsError) Error() string {
	n := len(e.FailedIndexes)
	if n == 0 {
		return "no items failed"
	}
	return fmt.Sprintf("%d item(s) failed, first (item %d): %v", n, e.FailedIndexes[0], e.itemErr(0))
}

// Unwrap exposes the per-item errors to errors.Is / errors.As.
func (e *PartialItemsError) Unwrap() []error { return e.Errs }

// itemErr is the error for FailedIndexes[i], tolerating a short Errs.
func (e *PartialItemsError) itemErr(i int) error {
	if i < len(e.Errs) && e.Errs[i] != nil {
		return e.Errs[i]
	}
	return fmt.Errorf("item %d failed", e.FailedIndexes[i])
}

// ComputeItemKey returns a stable key for one input item of a node run: the
// same execution, node, item position and item data always give the same key,
// so a node that calls out (sends, posts, charges) can skip an item it already
// handled when the run is retried or resumed.
func ComputeItemKey(executionID, nodeID string, index int, item Item) string {
	data, _ := json.Marshal(item.JSON) // map keys marshal sorted: deterministic
	h := sha256.New()
	for _, part := range [][]byte{[]byte(executionID), []byte(nodeID), []byte(strconv.Itoa(index)), data} {
		h.Write(part)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// ItemKey returns the idempotency key of in.Items[i]. When the engine retries
// a subset of items, ItemKeys carries each item's key from its original
// position; otherwise the key is computed from i.
func (in NodeInput) ItemKey(i int) string {
	if i < len(in.ItemKeys) {
		return in.ItemKeys[i]
	}
	return ComputeItemKey(in.ExecutionID, in.NodeID, i, in.Items[i])
}
