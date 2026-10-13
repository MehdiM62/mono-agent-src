package action

import (
	"context"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// 13. stepUpdateProgress — Increment a variable, update reached_index in db
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepUpdateProgress(ctx context.Context, step StepDef) (*StepResult, error) {
	// Apply variable assignments from the Set map.
	if step.Set != nil {
		for key, value := range step.Set {
			resolved := value // already resolved by ResolveStepDef; never twice
			ae.execCtx.SetVariable(key, resolved)
			ae.logger.Debug().
				Str("stepID", step.ID).
				Str("key", key).
				Interface("value", resolved).
				Msg("variable set")
		}
	}

	// Handle increment.
	if step.Increment != "" {
		val, _ := ae.execCtx.GetVariable(step.Increment)
		var newVal int
		switch v := val.(type) {
		case int:
			newVal = v + 1
		case float64:
			newVal = int(v) + 1
		default:
			newVal = 1
		}
		ae.execCtx.SetVariable(step.Increment, newVal)

		// Persist the reached index to storage.
		if ae.db != nil && ae.action != nil {
			if err := ae.db.UpdateActionReachedIndex(ae.action.ID, newVal); err != nil {
				ae.logger.Warn().Err(err).Int("index", newVal).Msg("failed to update reached index")
			}
		}

		ae.logger.Debug().
			Str("stepID", step.ID).
			Str("variable", step.Increment).
			Int("value", newVal).
			Msg("progress incremented")
	}

	return &StepResult{Success: true, StepID: step.ID}, nil
}

// ---------------------------------------------------------------------------
// 14. stepSaveData — Flush extracted data to storage
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepSaveData(ctx context.Context, step StepDef) (*StepResult, error) {
	ae.logger.Debug().
		Str("stepID", step.ID).
		Str("dataSource", step.DataSource).
		Msg("saving data")

	// Collect data from the data source variable or from the step result.
	var dataToSave []map[string]interface{}

	if step.DataSource != "" {
		val, ok := ae.execCtx.GetVariable(step.DataSource)
		if !ok {
			// Try step result.
			sr := ae.execCtx.GetStepResult(step.DataSource)
			if sr != nil {
				val = sr.Data
			}
		}

		if val != nil {
			switch d := val.(type) {
			case map[string]interface{}:
				dataToSave = append(dataToSave, d)
			case []map[string]interface{}:
				dataToSave = append(dataToSave, d...)
			case []interface{}:
				for _, item := range d {
					if m, ok := item.(map[string]interface{}); ok {
						dataToSave = append(dataToSave, m)
					}
				}
			}
		}

		// Track dataSource items that are not already recorded in the
		// extracted list. Producers like extract_multiple and
		// call_bot_method already call AddExtractedItem for their output, so
		// re-adding unconditionally would duplicate every row.
		ae.execCtx.mu.Lock()
		tracked := make([]map[string]interface{}, len(ae.execCtx.ExtractedItems))
		copy(tracked, ae.execCtx.ExtractedItems)
		ae.execCtx.mu.Unlock()
		// A single map outside a loop is one facet of the node's single
		// item and merges, as call_bot_method does; lists are records.
		// Legacy actions (no package / local-*) keep master's behaviour:
		// every saved row is a record.
		_, single := val.(map[string]interface{})
		record := !single || ae.execCtx.inLoop() || isLegacyPackage(ae.pkg)
		for _, item := range dataToSave {
			if containsExtractedItem(tracked, item) {
				continue
			}
			if record {
				ae.execCtx.AddRecord(item)
			} else {
				ae.execCtx.AddExtractedItem(item)
			}
		}
	}

	// If no specific data source, flush all accumulated extracted items.
	// These items are already tracked in the context — do not re-add them.
	if len(dataToSave) == 0 {
		ae.execCtx.mu.Lock()
		dataToSave = make([]map[string]interface{}, len(ae.execCtx.ExtractedItems))
		copy(dataToSave, ae.execCtx.ExtractedItems)
		ae.execCtx.mu.Unlock()
	}

	// Persist to storage.
	if ae.db != nil && ae.action != nil && len(dataToSave) > 0 {
		if err := ae.db.SaveExtractedData(ae.action.ID, ae.redactItems(dataToSave)); err != nil {
			ae.logger.Warn().Err(err).Msg("failed to save extracted data")
			return &StepResult{
				Success: false,
				StepID:  step.ID,
				Error:   fmt.Errorf("save_data: %w", err),
			}, nil
		}
	}

	ae.logger.Info().
		Str("stepID", step.ID).
		Int("itemCount", len(dataToSave)).
		Msg("data saved")

	return &StepResult{
		Success: true,
		Data:    len(dataToSave),
		StepID:  step.ID,
	}, nil
}

// ---------------------------------------------------------------------------
// 15. stepMarkFailed — Record a failure for the current loop item
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepMarkFailed(ctx context.Context, step StepDef) (*StepResult, error) {
	idx := 0
	if v, ok := ae.execCtx.GetVariable("loopIndex"); ok {
		switch i := v.(type) {
		case int:
			idx = i
		case float64:
			idx = int(i)
		}
	}

	errMsg := "step marked as failed"
	if step.Text != "" {
		errMsg = step.Text // resolved once by ResolveStepDef
	} else if step.Description != "" {
		errMsg = step.Description
	}

	ae.execCtx.AddFailedItem(FailedItem{
		StepID:    step.ID,
		Error:     fmt.Errorf("%s", errMsg),
		Timestamp: time.Now(),
		Index:     idx,
	})

	ae.logger.Warn().
		Str("stepID", step.ID).
		Int("index", idx).
		Str("reason", errMsg).
		Msg("item marked as failed")

	return &StepResult{Success: true, StepID: step.ID}, nil
}

// ---------------------------------------------------------------------------
// 16. stepSetVariable — Set an execution variable directly
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepSetVariable(_ context.Context, step StepDef) (*StepResult, error) {
	varName := step.VariableName
	if varName == "" {
		varName = step.Variable
	}
	if varName == "" {
		return &StepResult{Success: false, StepID: step.ID, Error: fmt.Errorf("set_variable step %s: missing variable_name", step.ID)}, nil
	}
	ae.execCtx.SetVariable(varName, step.Value)
	ae.logger.Debug().
		Str("stepID", step.ID).
		Str("type", "set_variable").
		Msg("variable set")
	return &StepResult{Success: true, StepID: step.ID}, nil
}

// ---------------------------------------------------------------------------
// 17. stepLog — Log a message with variable resolution
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepLog(ctx context.Context, step StepDef) (*StepResult, error) {
	msg := step.Text
	if msg == "" {
		msg = step.Description
	}
	if msg == "" {
		if s, ok := step.Value.(string); ok {
			msg = s
		}
	}

	resolved := msg // resolved (secrets masked) by ResolveStepDef; never twice

	ae.logger.Info().
		Str("stepID", step.ID).
		Str("message", resolved).
		Msg("action log")

	return &StepResult{Success: true, Data: resolved, StepID: step.ID}, nil
}

// ---------------------------------------------------------------------------
// 17. stepCallBotMethod — Call a method on the BotAdapter
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepCallBotMethod(ctx context.Context, step StepDef) (*StepResult, error) {
	if ae.botAdapter == nil {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("call_bot_method step %s: no bot adapter configured", step.ID),
		}, nil
	}

	methodName := step.MethodName
	if methodName == "" {
		methodName = step.Method
	}
	if methodName == "" {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("call_bot_method step %s: no method name specified", step.ID),
		}, nil
	}

	fn, ok := ae.botAdapter.GetMethodByName(methodName)
	if !ok {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("call_bot_method step %s: method %q not found on bot adapter", step.ID, methodName),
		}, nil
	}

	ae.logger.Debug().
		Str("stepID", step.ID).
		Str("method", methodName).
		Int("argCount", len(step.Args)).
		Msg("calling bot method")

	// Resolve arguments. The page (as PageInterface) is always prepended as
	// the first argument so bot methods have access to the browser page.
	resolvedArgs := []interface{}{ae.page}
	for _, arg := range step.Args {
		resolvedArgs = append(resolvedArgs, arg) // resolved by ResolveStepDef
	}

	// Honor the step's timeout, if configured.
	callCtx := ctx
	if step.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, time.Duration(step.Timeout*float64(time.Second)))
		defer cancel()
	}

	result, err := fn(callCtx, resolvedArgs...)
	if err != nil {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("call_bot_method %s/%s: %w", step.ID, methodName, err),
		}, nil
	}

	// Store the result in a variable if specified.
	varName := step.VariableName
	if varName == "" {
		varName = step.Variable
	}
	if varName != "" {
		ae.execCtx.SetVariable(varName, result)
	}

	// If the bot method returned a map, or a list of maps (list_* methods),
	// also add it as extracted item(s) so it appears in the node output.
	// save_data over the same variable skips rows already tracked here.
	// A list result is records; so is one map per loop iteration (e.g. a
	// profile scraped per target). A single map outside a loop is one facet
	// of the node's single item (the Gemini shape) and merges.
	_, isMap := result.(map[string]interface{})
	record := !isMap || ae.execCtx.inLoop()
	for _, m := range botResultItems(result) {
		if record {
			ae.execCtx.AddRecord(m)
		} else {
			ae.execCtx.AddExtractedItem(m)
		}
	}

	ae.logger.Debug().
		Str("stepID", step.ID).
		Str("method", methodName).
		Msg("bot method executed successfully")

	return &StepResult{
		Success: true,
		Data:    result,
		StepID:  step.ID,
	}, nil
}

// botResultItems returns the extracted rows a bot method result carries: a
// non-empty map, or every map of a list result ([]map or []interface{}
// holding maps). List rows are taken exactly as save_data would take them
// from the same variable, so save_data's already-tracked check matches them
// and nothing is recorded twice. Anything else yields no rows.
func botResultItems(result interface{}) []map[string]interface{} {
	switch r := result.(type) {
	case map[string]interface{}:
		if len(r) > 0 {
			return []map[string]interface{}{r}
		}
	case []map[string]interface{}:
		return r
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(r))
		for _, v := range r {
			if m, ok := v.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}
