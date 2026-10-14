package action

import "strings"

// A run's target list is the list a node hands the action as
// selectedListItems (also seeded as "targets"). A loop or for_each over it
// processes one target per iteration; the executor tracks which targets
// completed and which failed so a caller can retry only the failed ones.

// iteratesTargets reports whether a loop iterator (or for_each items)
// expression walks the run's target list: it names selectedListItems or
// targets first (e.g. "selectedListItems or foundPosts") and that list is
// not empty, so the expression resolves to it.
func (ae *ActionExecutor) iteratesTargets(expr string) bool {
	expr = strings.TrimSpace(expr)
	expr = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(expr, "{{"), "}}"))
	name := expr
	if i := strings.IndexAny(expr, " \t|"); i >= 0 {
		name = expr[:i]
	}
	if name != "selectedListItems" && name != "targets" {
		return false
	}
	list, _ := toList(ae.resolver.ResolvePath(name))
	return len(list) > 0
}

// beginTarget marks target idx as being processed. It reports false, changing
// nothing, inside another target iteration: the outer one keeps the target.
func (ec *ExecutionContext) beginTarget(idx int) bool {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	if ec.targetPos > 0 {
		return false
	}
	ec.targetPos = idx + 1
	return true
}

// endTarget ends the current target iteration; completed means its steps ran
// to the end (a failure they recorded still makes the target failed).
func (ec *ExecutionContext) endTarget(completed bool) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	if completed && ec.targetPos > 0 {
		ec.completedTargets = append(ec.completedTargets, ec.targetPos-1)
	}
	ec.targetPos = 0
}

// runTarget runs body as the iteration over target idx when tracked,
// recording whether it completed: no error and no safe-mode stop.
func (ae *ActionExecutor) runTarget(tracked bool, idx int, body func() error) error {
	if !tracked || !ae.execCtx.beginTarget(idx) {
		return body()
	}
	err := body()
	ae.execCtx.endTarget(err == nil && ae.safeStop == nil)
	return err
}

// succeededTargets returns the completed targets no failure was recorded
// for. The caller holds ec.mu.
func (ec *ExecutionContext) succeededTargets() []int {
	failed := map[int]bool{}
	for _, f := range ec.FailedItems {
		if f.Target {
			failed[f.Index] = true
		}
	}
	var out []int
	for _, i := range ec.completedTargets {
		if !failed[i] {
			out = append(out, i)
		}
	}
	return out
}
