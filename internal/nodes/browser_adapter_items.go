package nodes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/monoes/mono-agent/internal/action"
	"github.com/monoes/mono-agent/internal/workflow"
)

// A BrowserNode run's targets come from its config ("targets", resolved once
// for the whole input against its first item), not one per input item, so a
// failed target cannot be mapped to the input item it came from. Retries are
// deduped by target instead: targetProgress remembers which targets already
// succeeded and the next attempt runs only the others. A run in which some
// targets succeeded and others failed returns a *workflow.PartialItemsError
// with the succeeded targets' outputs and every input item named failed; the
// engine retries those items (the node then skips the targets already done)
// and merges the outputs of all attempts.

// targetProgress tracks the targets of one node run across its attempts. It
// is kept in the node's state (workflow.SetNodeState): per execution and
// node, it survives the engine's retries, a pause and a process restart, and
// it is cleared once the run succeeds.
type targetProgress struct {
	ctx   context.Context
	state targetState
	run   []int // full-list index of each target this attempt runs
}

type targetState struct {
	List string `json:"targets"` // fingerprint of the full target list
	Done []int  `json:"done"`    // full-list indexes of succeeded targets
}

// newTargetProgress loads the progress saved for this target list. A saved
// state for a different list (a later loop pass, a changed config) is
// ignored.
func newTargetProgress(ctx context.Context, targets []interface{}) *targetProgress {
	p := &targetProgress{ctx: ctx, state: targetState{List: targetsFingerprint(targets)}}
	var saved targetState
	if raw := workflow.NodeStateFromContext(ctx); raw != nil &&
		json.Unmarshal(raw, &saved) == nil && saved.List == p.state.List {
		p.state.Done = saved.Done
	}
	return p
}

func targetsFingerprint(targets []interface{}) string {
	data, _ := json.Marshal(targets) // map keys marshal sorted: deterministic
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

// remaining returns the targets not done yet, in order.
func (p *targetProgress) remaining(targets []interface{}) []interface{} {
	done := make(map[int]bool, len(p.state.Done))
	for _, i := range p.state.Done {
		done[i] = true
	}
	var out []interface{}
	p.run = p.run[:0]
	for i, t := range targets {
		if !done[i] {
			out = append(out, t)
			p.run = append(p.run, i)
		}
	}
	return out
}

// record marks the attempt's succeeded targets done and saves the progress.
// It returns how many targets succeeded in this attempt.
func (p *targetProgress) record(result *action.ExecutionResult) int {
	n := 0
	for _, i := range result.SucceededTargets {
		if i >= 0 && i < len(p.run) {
			p.state.Done = append(p.state.Done, p.run[i])
			n++
		}
	}
	if n > 0 {
		_ = workflow.SetNodeState(p.ctx, p.state) // no slot outside an execution: nothing to keep
	}
	return n
}

// finish clears the progress once the run succeeded.
func (p *targetProgress) finish() {
	_ = workflow.SetNodeState(p.ctx, nil)
}

// partialFailure returns the *workflow.PartialItemsError of an attempt in
// which succeeded > 0 targets worked and the action still failed, or nil when
// nothing succeeded or there is no input item to name (the plain error then
// stands; the retry still skips the targets done).
func (b *BrowserNode) partialFailure(input workflow.NodeInput, ex *action.ActionExecutor, result *action.ExecutionResult, succeeded int, err error) error {
	if succeeded == 0 || result == nil || len(input.Items) == 0 {
		return nil
	}
	ok := make(map[int]bool, len(result.SucceededTargets))
	for _, i := range result.SucceededTargets {
		ok[i] = true
	}
	kept := *result
	kept.ExtractedItems = nil
	for i, item := range result.ExtractedItems {
		if t := extractedTarget(result, i); t < 0 || ok[t] {
			kept.ExtractedItems = append(kept.ExtractedItems, item)
		}
	}
	outs, passThrough := b.outputs(input, ex, &kept)
	if passThrough {
		// The input items are not done: they pass through once the retry
		// finishes the remaining targets.
		outs = nil
	}
	runErr := fmt.Errorf("nodes: BrowserNode execute %s/%s: %d target(s) done, the rest failed: %w", b.platform, b.actionType, succeeded, err)
	pe := &workflow.PartialItemsError{Succeeded: outs}
	for i := range input.Items {
		pe.FailedIndexes = append(pe.FailedIndexes, i)
		pe.Errs = append(pe.Errs, runErr)
	}
	return pe
}

// extractedTarget is the target ExtractedItems[i] was produced for, -1 when
// none.
func extractedTarget(result *action.ExecutionResult, i int) int {
	if i < len(result.ExtractedTargets) {
		return result.ExtractedTargets[i]
	}
	return -1
}

// outputs converts an action's result to the node's outputs. passThrough
// reports that nothing was extracted or declared and the input items are
// passed through as they came.
func (b *BrowserNode) outputs(input workflow.NodeInput, ex *action.ActionExecutor, result *action.ExecutionResult) (outs []workflow.NodeOutput, passThrough bool) {
	// Convert and normalize ExtractedItems to a single output item.
	// All extracted items (one per bot method step) are merged together so the
	// downstream node sees a single item with all fields — including both the
	// input data (sheet row, prompts) and the action results (image_count,
	// response_text, etc.).  This prevents "3 items for 3 steps" fan-out.
	var inputJSON map[string]interface{}
	if len(input.Items) > 0 {
		inputJSON = input.Items[0].JSON
	}

	// List actions (comments, posts, followers, search results, one profile
	// per target) produce records: emit one item per record. Before, they
	// were merged into a single item and only the last record survived.
	if result.ListOutput && len(result.ExtractedItems) > 0 {
		return []workflow.NodeOutput{
			{Handle: "main", Items: recordItems(result.ExtractedItems, b.platform)},
		}, false
	}

	// A declarative package's action also reports what it declared in
	// outputs.success (variables it set); native built-ins get nil here.
	declared := declaredOutputs(ex, b.platform, b.actionType)

	if len(result.ExtractedItems) > 0 {
		merged := withDeclaredOutputs(mergeStepResults(inputJSON, result.ExtractedItems, b.platform), declared)
		return []workflow.NodeOutput{
			{Handle: "main", Items: []workflow.Item{workflow.NewItem(merged)}},
		}, false
	}

	if declared != nil {
		return []workflow.NodeOutput{
			{Handle: "main", Items: []workflow.Item{workflow.NewItem(withDeclaredOutputs(inputJSON, declared))}},
		}, false
	}

	return passThroughOutputs(input), true
}

// passThroughOutputs passes the input items through (e.g. publish actions,
// which extract nothing).
func passThroughOutputs(input workflow.NodeInput) []workflow.NodeOutput {
	if len(input.Items) > 0 {
		return []workflow.NodeOutput{
			{Handle: "main", Items: input.Items},
		}
	}
	return []workflow.NodeOutput{
		{Handle: "main", Items: []workflow.Item{}},
	}
}
