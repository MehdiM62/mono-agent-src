package action

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// executeLoop runs a single loop definition. It resolves the iterator to a
// slice, then executes the loop's steps for each item starting from
// the action's reached index. If the loop declares a maxItems cap, at most
// that many items are processed per session before the loop exits with a
// "cap reached" note.
func (ae *ActionExecutor) executeLoop(ctx context.Context, loop LoopDef, allSteps []StepDef) error {
	// Resolve the iterator to get the collection to iterate over.
	items := ae.resolveIterator(loop.Iterator)
	if items == nil {
		ae.logger.Warn().
			Str("loopID", loop.ID).
			Str("iterator", loop.Iterator).
			Msg("loop iterator resolved to nil, skipping loop")
		return nil
	}

	collection := toSlice(items)
	ae.execCtx.enterLoop()
	defer ae.execCtx.leaveLoop()
	if len(collection) == 0 {
		// toSlice yields nil both for a genuinely empty collection and for a
		// value that simply isn't one (a string, a number). Only the first is
		// legitimately zero work; the second is a misconfigured iterator that
		// otherwise runs no steps, produces nothing, and still reports
		// success — how a Gemini batch whose prompts rendered to the string
		// "null" came back green with no images.
		if !isIterable(items) {
			return fmt.Errorf("loop %q: iterator %q resolved to %T (%v), which cannot be iterated — expected an array",
				loop.ID, loop.Iterator, items, truncateForError(items))
		}
		ae.logger.Info().Str("loopID", loop.ID).Msg("loop collection is empty")
		return nil
	}

	// Determine the starting index (for resume support), keyed per loop ID:
	// a loop that has already run in this execution resumes from its own
	// progress; a loop's first run seeds from the action-level reached index.
	startIdx := 0
	if reached, ok := ae.reachedIndexByLoop[loop.ID]; ok {
		startIdx = reached
	} else if ae.action != nil && ae.action.ReachedIndex > 0 {
		startIdx = ae.action.ReachedIndex
	}

	// Resolve the per-session item cap (0 or unresolvable → unlimited).
	maxItems := ae.resolveLoopCap(loop.MaxItems)

	// Resolve the daily cap (0 or unresolvable → unlimited) and the profile
	// identity it's tracked against. The daily counter persists across
	// separate action runs (unlike maxItems, which resets every run), so it
	// requires storage; with no db configured (e.g. some tests) the daily
	// cap is a no-op, same as the session cap already is in that case.
	dailyCap := ae.resolveLoopCap(loop.MaxItemsPerDay)

	loopSteps := ae.getStepsByIDs(allSteps, loop.Steps)

	ae.logger.Info().
		Str("loopID", loop.ID).
		Int("total", len(collection)).
		Int("startIndex", startIdx).
		Int("maxItems", maxItems).
		Int("dailyCap", dailyCap).
		Int("stepCount", len(loopSteps)).
		Msg("starting loop execution")

	processed := 0
	for idx := startIdx; idx < len(collection); idx++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Per-session cap: stop before processing more items than allowed.
		if maxItems > 0 && processed >= maxItems {
			ae.noteLoopCapReached(loop, maxItems, idx)
			ae.reachedIndexByLoop[loop.ID] = idx
			// Persist the cap boundary so a later run (e.g. with a raised
			// cap) resumes from here instead of redoing capped items.
			if ae.db != nil && ae.action != nil {
				if err := ae.db.UpdateActionReachedIndex(ae.action.ID, idx); err != nil {
					ae.logger.Warn().Err(err).Int("index", idx).Msg("failed to update reached index at cap")
				}
			}
			return nil
		}

		// Daily cap: stop before processing more items than allowed today
		// (UTC), across however many separate action runs got here.
		if dailyCap > 0 && ae.db != nil && ae.action != nil {
			count, err := ae.db.GetDailyActionCount(ae.action.Type)
			if err != nil {
				ae.logger.Warn().Err(err).Msg("failed to read daily action count; treating as unlimited for this iteration")
			} else if count >= dailyCap {
				ae.noteDailyCapReached(loop, dailyCap, count, idx)
				ae.reachedIndexByLoop[loop.ID] = idx
				if err := ae.db.UpdateActionReachedIndex(ae.action.ID, idx); err != nil {
					ae.logger.Warn().Err(err).Int("index", idx).Msg("failed to update reached index at daily cap")
				}
				return nil
			}
		}

		item := collection[idx]

		// Set loop variables.
		ae.execCtx.SetVariable("item", item)
		ae.execCtx.SetVariable(loop.IndexVar, idx)
		ae.execCtx.SetVariable("loopIndex", idx)
		ae.execCtx.SetVariable("loopTotal", len(collection))

		ae.emitEvent(ExecutionEvent{
			Type:     "loop_iteration",
			ActionID: ae.action.ID,
			StepID:   loop.ID,
			Index:    idx,
			Total:    len(collection),
			Message:  fmt.Sprintf("Loop %s: item %d/%d", loop.ID, idx+1, len(collection)),
		})

		stepErr := ae.executeSteps(ctx, loopSteps)
		if stepErr != nil {
			if isHalt(stepErr) {
				return stepErr
			}
			ae.logger.Warn().
				Err(stepErr).
				Int("index", idx).
				Str("loopID", loop.ID).
				Msg("loop iteration failed")
		}

		// Only a successfully completed iteration counts against the daily
		// cap — a failed step (e.g. a follow button that never appeared)
		// didn't actually perform the rate-limited action.
		if stepErr == nil && dailyCap > 0 && ae.db != nil && ae.action != nil {
			if _, err := ae.db.IncrementDailyActionCount(ae.action.Type); err != nil {
				ae.logger.Warn().Err(err).Msg("failed to increment daily action count")
			}
		}

		processed++
		ae.reachedIndexByLoop[loop.ID] = idx + 1

		// Update reached index in storage for resume support.
		// Batch writes: persist every 50 iterations or on the last item to avoid N+1 DB writes.
		if ae.db != nil && ae.action != nil {
			if (idx+1)%50 == 0 || idx == len(collection)-1 {
				if err := ae.db.UpdateActionReachedIndex(ae.action.ID, idx+1); err != nil {
					ae.logger.Warn().Err(err).Int("index", idx+1).Msg("failed to update reached index")
				}
			}
		}
	}

	return nil
}

// resolveLoopCap resolves a loop's maxItems template (e.g.
// "{{maxMessagesPerSession or 30}}") to an integer cap. Empty, missing, or
// non-numeric values resolve to 0, meaning unlimited.
func (ae *ActionExecutor) resolveLoopCap(template string) int {
	if strings.TrimSpace(template) == "" {
		return 0
	}
	return toInt(ae.resolver.ResolveValue(template))
}

// toInt coerces common numeric types (including numeric strings from CLI
// params) to an int, returning 0 for anything unrecognised.
func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return 0
		}
		return i
	default:
		return 0
	}
}

// noteLoopCapReached records that a loop exited early because its per-session
// cap was reached. The note is logged, emitted as an event for monitors, and
// stored in the execution context (Data + Variables) so it surfaces in the
// action's result metadata.
func (ae *ActionExecutor) noteLoopCapReached(loop LoopDef, cap, idx int) {
	note := map[string]interface{}{
		"loopId":     loop.ID,
		"cap":        cap,
		"startIndex": idx,
		"reason":     "per-session cap reached",
	}
	ae.execCtx.SetData("loopCapReached", note)
	ae.execCtx.SetVariable("capReached", loop.ID)
	ae.logger.Info().
		Str("loopID", loop.ID).
		Int("cap", cap).
		Int("index", idx).
		Msg("loop stopped: per-session cap reached")
	ae.emitEvent(ExecutionEvent{
		Type:     "loop_cap_reached",
		ActionID: ae.action.ID,
		StepID:   loop.ID,
		Index:    idx,
		Message:  fmt.Sprintf("Loop %s stopped at item %d: per-session cap of %d reached", loop.ID, idx, cap),
	})
}

// noteDailyCapReached records that a loop exited early because its
// persisted daily cap (across action runs, resetting at UTC midnight) was
// reached — the counterpart to noteLoopCapReached's per-session cap.
func (ae *ActionExecutor) noteDailyCapReached(loop LoopDef, cap, count, idx int) {
	note := map[string]interface{}{
		"loopId":     loop.ID,
		"cap":        cap,
		"count":      count,
		"startIndex": idx,
		"reason":     "daily cap reached",
	}
	ae.execCtx.SetData("dailyCapReached", note)
	ae.execCtx.SetVariable("dailyCapReached", loop.ID)
	ae.logger.Info().
		Str("loopID", loop.ID).
		Int("cap", cap).
		Int("count", count).
		Int("index", idx).
		Msg("loop stopped: daily cap reached")
	ae.emitEvent(ExecutionEvent{
		Type:     "loop_cap_reached",
		ActionID: ae.action.ID,
		StepID:   loop.ID,
		Index:    idx,
		Message:  fmt.Sprintf("daily cap reached: %d/%d %s today", count, cap, ae.action.Type),
	})
}

// resolveIterator resolves the loop iterator path to its underlying value.
func (ae *ActionExecutor) resolveIterator(iteratorPath string) interface{} {
	return ae.resolver.ResolvePath(iteratorPath)
}

// toSlice converts various collection types to []interface{}.
// isIterable reports whether val is one of the collection shapes toSlice
// understands — i.e. whether an empty result from toSlice means "no items"
// rather than "not a collection at all".
func isIterable(val interface{}) bool {
	switch val.(type) {
	case []interface{}, []string, []map[string]interface{}:
		return true
	}
	return false
}

// truncateForError keeps a misconfigured iterator's value readable in an
// error without pasting a whole document into it.
func truncateForError(v interface{}) string {
	s := fmt.Sprintf("%v", v)
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

func toSlice(val interface{}) []interface{} {
	switch v := val.(type) {
	case []interface{}:
		return v
	case []string:
		result := make([]interface{}, len(v))
		for i, s := range v {
			result[i] = s
		}
		return result
	case []map[string]interface{}:
		result := make([]interface{}, len(v))
		for i, m := range v {
			result[i] = m
		}
		return result
	default:
		return nil
	}
}

// getAllReferencedIDs collects all step IDs referenced by a loop, including
// those in then/else branches of condition steps.
func (ae *ActionExecutor) getAllReferencedIDs(loop LoopDef, allSteps []StepDef) []string {
	// Build a lookup map once instead of scanning allSteps per ID.
	stepMap := make(map[string]StepDef, len(allSteps))
	for _, step := range allSteps {
		stepMap[step.ID] = step
	}

	seen := make(map[string]bool)
	// Iterative BFS instead of recursion to avoid stack overflow on deeply nested actions.
	queue := make([]string, len(loop.Steps))
	copy(queue, loop.Steps)

	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		if step, ok := stepMap[id]; ok {
			if len(step.Then) > 0 {
				queue = append(queue, step.Then...)
			}
			if len(step.Else) > 0 {
				queue = append(queue, step.Else...)
			}
		}
	}

	result := make([]string, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	return result
}

// getStepsByIDs returns step definitions from allSteps matching the given IDs,
// preserving the order of the ids slice.
func (ae *ActionExecutor) getStepsByIDs(allSteps []StepDef, ids []string) []StepDef {
	stepMap := make(map[string]StepDef, len(allSteps))
	for _, step := range allSteps {
		stepMap[step.ID] = step
	}

	result := make([]StepDef, 0, len(ids))
	for _, id := range ids {
		if step, ok := stepMap[id]; ok {
			result = append(result, step)
		} else {
			ae.logger.Warn().
				Str("stepID", id).
				Msg("referenced step id has no definition, dropping reference")
		}
	}
	return result
}
