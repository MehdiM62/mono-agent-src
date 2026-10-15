package action

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// 12. stepCondition — Evaluate a condition and execute then/else branches
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepCondition(ctx context.Context, step StepDef) (*StepResult, error) {
	condResult := ae.evaluateCondition(step)

	ae.logger.Debug().
		Str("stepID", step.ID).
		Bool("result", condResult).
		Msg("condition evaluated")

	// Execute the appropriate branch.
	var branchIDs []string
	if condResult {
		branchIDs = step.Then
	} else {
		branchIDs = step.Else
	}

	if len(branchIDs) == 0 {
		return &StepResult{Success: true, Data: condResult, StepID: step.ID}, nil
	}

	// Prevent infinite recursion. The counter is scoped to the current
	// outer-loop iteration (when running inside a loop) so that a long loop
	// does not exhaust the cap across iterations — each iteration gets a
	// fresh budget while runaway recursion within one iteration is still
	// caught.
	recursionKey := fmt.Sprintf("condition_%s", step.ID)
	if idx, ok := ae.execCtx.GetVariable("loopIndex"); ok {
		recursionKey = fmt.Sprintf("condition_%s_loopidx_%v", step.ID, idx)
	}
	count := ae.execCtx.IncrementRecursion(recursionKey)
	const maxRecursion = 100
	if count > maxRecursion {
		ae.logger.Warn().
			Str("stepID", step.ID).
			Int("count", count).
			Msg("max recursion depth reached for condition")
		return &StepResult{Success: true, Data: condResult, StepID: step.ID}, nil
	}

	// For pagination loops (checking resultsCount < maxResultsCount), if no new
	// items were added since the previous iteration, break early rather than
	// continuously scrolling an ended page.
	condStr, isStr := step.Condition.(string)
	if isStr && strings.Contains(condStr, "resultsCount") && count > 1 {
		lastCountKey := fmt.Sprintf("last_extracted_%s", recursionKey)
		currentExtracted := len(ae.execCtx.ExtractedItems)
		if prevVal, ok := ae.execCtx.GetVariable(lastCountKey); ok {
			if prevCount, ok := prevVal.(int); ok && currentExtracted <= prevCount {
				ae.logger.Debug().
					Str("stepID", step.ID).
					Int("current", currentExtracted).
					Int("prev", prevCount).
					Msg("pagination reached end of results — ending pagination loop early")
				return &StepResult{Success: true, Data: false, StepID: step.ID}, nil
			}
		}
		ae.execCtx.SetVariable(lastCountKey, currentExtracted)
	} else if isStr && strings.Contains(condStr, "resultsCount") {
		lastCountKey := fmt.Sprintf("last_extracted_%s", recursionKey)
		ae.execCtx.SetVariable(lastCountKey, len(ae.execCtx.ExtractedItems))
	}

	// Resolve branch IDs to steps and loops, and execute in order. A branch id
	// may reference either a regular step or a loop id (e.g. a tier-1 failure
	// branch kicking off a per-item fallback loop). Consecutive step ids are
	// batched into a single executeSteps call, matching prior behavior.
	loopByID := make(map[string]LoopDef, len(ae.actionDef.Loops))
	for _, loop := range ae.actionDef.Loops {
		loopByID[loop.ID] = loop
	}

	runStepBatch := func(ids []string) error {
		if len(ids) == 0 {
			return nil
		}
		return ae.executeSteps(ctx, ae.getStepsByIDs(ae.actionDef.Steps, ids))
	}

	var stepBatch []string
	for _, id := range branchIDs {
		loop, isLoop := loopByID[id]
		if !isLoop {
			stepBatch = append(stepBatch, id)
			continue
		}
		if err := runStepBatch(stepBatch); err != nil {
			return &StepResult{
				Success: false,
				StepID:  step.ID,
				Error:   fmt.Errorf("condition %s branch execution: %w", step.ID, err),
			}, nil
		}
		stepBatch = nil
		if err := ae.executeLoop(ctx, loop, ae.actionDef.Steps); err != nil {
			return &StepResult{
				Success: false,
				StepID:  step.ID,
				Error:   fmt.Errorf("condition %s branch loop %s: %w", step.ID, id, err),
			}, nil
		}
	}
	if err := runStepBatch(stepBatch); err != nil {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("condition %s branch execution: %w", step.ID, err),
		}, nil
	}

	return &StepResult{Success: true, Data: condResult, StepID: step.ID}, nil
}

// evaluateCondition parses and evaluates the condition expression from a step.
// It supports both string expressions (e.g. "variable == 'value'") and
// ConditionDef objects with operators: "exists", "equals", "not_equals",
// "greater_than", "contains".
func (ae *ActionExecutor) evaluateCondition(step StepDef) bool {
	if step.Condition == nil {
		return false
	}

	// Try to parse as ConditionDef struct first.
	switch v := step.Condition.(type) {
	case map[string]interface{}:
		cond := parseConditionDef(v)
		if cond != nil {
			return ae.evalConditionDef(cond)
		}
	case *ConditionDef:
		return ae.evalConditionDef(v)
	case ConditionDef:
		return ae.evalConditionDef(&v)
	}

	// Fall back to string expression evaluation.
	cond, ok := step.Condition.(string)
	if !ok {
		// Try JSON round-trip for other types.
		data, err := json.Marshal(step.Condition)
		if err != nil {
			return false
		}
		var condDef ConditionDef
		if err := json.Unmarshal(data, &condDef); err == nil && condDef.Variable != "" {
			return ae.evalConditionDef(&condDef)
		}
		return false
	}

	// Handle compound conditions with "and" / "or".
	if strings.Contains(cond, " and ") {
		parts := strings.Split(cond, " and ")
		for _, part := range parts {
			if !ae.evalSimpleCondition(strings.TrimSpace(part)) {
				return false
			}
		}
		return true
	}
	if strings.Contains(cond, " or ") {
		parts := strings.Split(cond, " or ")
		for _, part := range parts {
			if ae.evalSimpleCondition(strings.TrimSpace(part)) {
				return true
			}
		}
		return false
	}

	return ae.evalSimpleCondition(cond)
}

// parseConditionDef converts a map[string]interface{} into a ConditionDef.
func parseConditionDef(m map[string]interface{}) *ConditionDef {
	cond := &ConditionDef{}
	if v, ok := m["variable"].(string); ok {
		cond.Variable = v
	}
	if v, ok := m["operator"].(string); ok {
		cond.Operator = v
	}
	cond.Value = m["value"]

	if cond.Variable == "" {
		return nil
	}
	return cond
}

// evalConditionDef evaluates a structured ConditionDef against the execution context.
func (ae *ActionExecutor) evalConditionDef(cond *ConditionDef) bool {
	varValue, exists := ae.execCtx.GetVariable(cond.Variable)

	switch cond.Operator {
	case "exists":
		return exists && varValue != nil

	case "not_exists":
		return !exists || varValue == nil

	case "equals":
		if !exists {
			return false
		}
		return fmt.Sprintf("%v", varValue) == fmt.Sprintf("%v", cond.Value)

	case "not_equals":
		if !exists {
			return true
		}
		return fmt.Sprintf("%v", varValue) != fmt.Sprintf("%v", cond.Value)

	case "greater_than":
		if !exists {
			return false
		}
		return toFloat64(varValue) > toFloat64(cond.Value)

	case "contains":
		if !exists {
			return false
		}
		return strings.Contains(fmt.Sprintf("%v", varValue), fmt.Sprintf("%v", cond.Value))

	default:
		ae.logger.Warn().
			Str("operator", cond.Operator).
			Msg("unknown condition operator, defaulting to false")
		return false
	}
}

// toFloat64 converts a value to float64 for numeric comparisons.
func toFloat64(val interface{}) float64 {
	switch v := val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	default:
		return 0
	}
}

// evalSimpleCondition evaluates a single condition expression like
// "variable == 'value'" or "variable != ”".
func (ae *ActionExecutor) evalSimpleCondition(expr string) bool {
	expr = strings.TrimSpace(expr)

	// Parse operator.
	operators := []struct {
		op   string
		eval func(left, right string) bool
	}{
		{"!=", func(l, r string) bool { return l != unquote(r) }},
		{"==", func(l, r string) bool { return l == unquote(r) }},
		{">=", func(l, r string) bool { return toFloat(l) >= toFloat(r) }},
		{"<=", func(l, r string) bool { return toFloat(l) <= toFloat(r) }},
		{">", func(l, r string) bool { return toFloat(l) > toFloat(r) }},
		{"<", func(l, r string) bool { return toFloat(l) < toFloat(r) }},
	}

	for _, op := range operators {
		if idx := strings.Index(expr, op.op); idx > 0 {
			left := strings.TrimSpace(expr[:idx])
			right := strings.TrimSpace(expr[idx+len(op.op):])

			// Resolve both sides: the left as a variable reference, the right
			// as a literal (quoted/numeric/boolean) or, failing that, a
			// variable reference. Without RHS resolution, comparisons like
			// "resultsCount < maxResultsCount" compare against toFloat of the
			// raw variable name, which is always 0.
			leftVal := ae.resolveConditionValue(left)
			rightVal := ae.resolveConditionOperand(right)

			return op.eval(leftVal, rightVal)
		}
	}

	// No operator found — treat as a truthiness check.
	val := ae.resolveConditionValue(expr)
	return val != "" && val != "0" && val != "false" && val != "None" && val != "nil"
}

// resolveConditionValue resolves a variable name to its string value for
// condition evaluation.
func (ae *ActionExecutor) resolveConditionValue(name string) string {
	val := ae.resolver.ResolvePath(name)
	if val == nil {
		return ""
	}
	return fmt.Sprintf("%v", val)
}

// resolveConditionOperand resolves the right-hand side of a simple condition.
// Quoted string literals, numeric literals, and boolean/null literals are kept
// as-is; anything else is treated as a variable reference and resolved through
// the execution context. If no such variable exists, the raw text is returned
// unchanged (preserving pre-existing behavior for unknown names).
func (ae *ActionExecutor) resolveConditionOperand(right string) string {
	trimmed := strings.TrimSpace(right)
	unq := unquote(trimmed)
	if unq != trimmed {
		// Quoted string literal (e.g. 'value' or "value").
		return unq
	}
	if _, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return trimmed
	}
	switch trimmed {
	case "true", "false", "None", "nil":
		return trimmed
	}
	if v := ae.resolver.ResolvePath(trimmed); v != nil {
		return fmt.Sprintf("%v", v)
	}
	return trimmed
}
