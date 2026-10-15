package action

import (
	"context"
	"errors"
	"fmt"
)

// executeSteps runs a slice of steps sequentially. It handles variable
// resolution, error handling, and retries for each step.
func (ae *ActionExecutor) executeSteps(ctx context.Context, steps []StepDef) error {
	for i := 0; i < len(steps); i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := ae.haltErr(); err != nil {
			return err
		}
		step := steps[i]
		if ae.safeMode && ae.safeStopRequired(step) {
			if err := ae.precheckSafeStop(ctx, step); err != nil {
				return ae.failRun(step.ID, err)
			}
			return ae.stopBeforeSideEffect(step)
		}

		// Resolve templates in the step definition.
		resolved := ae.resolver.ResolveStepDef(step)

		handler, ok := ae.handlers[resolved.Type]
		if !ok {
			return ae.failRun(resolved.ID, fmt.Errorf("unknown step type %q", resolved.Type))
		}

		ae.emitEvent(ExecutionEvent{
			Type:     "step_start",
			ActionID: ae.action.ID,
			StepID:   resolved.ID,
			Index:    i,
			Total:    len(steps),
			Message:  fmt.Sprintf("Executing step %s (%s)", resolved.ID, resolved.Type),
		})

		result, err := handler(ctx, resolved)
		if err == nil && result != nil && result.Success && result.Error == nil && !result.Skip && !result.Abort && ae.SuccessfulStep != nil {
			ae.SuccessfulStep(resolved, result)
		}
		result, err = ae.applyUntil(ctx, resolved, result, err)
		result, err = ae.redactStep(result, err)
		if herr := ae.afterStep(resolved, result, err); herr != nil {
			return herr
		}
		if err != nil || (result != nil && !result.Success) {
			if result == nil {
				result = &StepResult{
					Success: false,
					StepID:  resolved.ID,
					Error:   err,
				}
			}
			result.StepID = resolved.ID

			// Apply error handler if defined.
			onErr := resolved.OnError
			if onErr == nil {
				onErr = ae.defaultOnError()
			}
			handled := ae.errorHandler.Handle(ctx, onErr, result, ae.execCtx)

			if handled.Abort {
				if errors.Is(handled.Error, ErrAbort) {
					return handled.Error
				}
				return ErrAbort
			}
			if handled.Retry {
				// Re-execute the same step.
				i--
				continue
			}
			if handled.Skip {
				skipLog := ae.logger.Debug().Str("stepID", resolved.ID)
				if result != nil && result.Error != nil {
					skipLog = skipLog.Err(result.Error)
				}
				skipLog.Msg("step skipped after error handling")
				ae.execCtx.SetStepResult(resolved.ID, handled)
				continue
			}

			// For "continue" error action, record and move on.
			ae.execCtx.SetStepResult(resolved.ID, handled)
			continue
		}

		// Step succeeded.
		if result != nil {
			result.StepID = resolved.ID
			ae.execCtx.SetStepResult(resolved.ID, result)

			// Handle onSuccess callback.
			if resolved.OnSuccess != nil {
				ae.handleOnSuccess(resolved.OnSuccess)
			}
		}

		// Reset retry counter on success.
		ae.errorHandler.ResetRetries(resolved.ID)

		ae.emitEvent(ExecutionEvent{
			Type:     "step_complete",
			ActionID: ae.action.ID,
			StepID:   resolved.ID,
			Index:    i,
			Total:    len(steps),
		})
	}
	return nil
}

// handleOnSuccess processes the onSuccess callback of a step.
func (ae *ActionExecutor) handleOnSuccess(sa *SuccessAction) {
	switch sa.Action {
	case "set_variable":
		if sa.Variable != "" {
			ae.execCtx.SetVariable(sa.Variable, sa.Value)
		}
	case "increment":
		varName := sa.Increment
		if varName == "" {
			varName = sa.Variable
		}
		if varName != "" {
			val, _ := ae.execCtx.GetVariable(varName)
			switch v := val.(type) {
			case int:
				ae.execCtx.SetVariable(varName, v+1)
			case float64:
				ae.execCtx.SetVariable(varName, v+1)
			default:
				ae.execCtx.SetVariable(varName, 1)
			}
		}
	case "update_progress":
		if sa.Increment != "" {
			val, _ := ae.execCtx.GetVariable(sa.Increment)
			switch v := val.(type) {
			case int:
				ae.execCtx.SetVariable(sa.Increment, v+1)
			case float64:
				ae.execCtx.SetVariable(sa.Increment, v+1)
			default:
				ae.execCtx.SetVariable(sa.Increment, 1)
			}
		}
	case "save_data":
		// Trigger a flush of extracted items.
		if ae.db != nil && ae.action != nil && len(ae.execCtx.ExtractedItems) > 0 {
			ae.execCtx.mu.Lock()
			items := make([]map[string]interface{}, len(ae.execCtx.ExtractedItems))
			copy(items, ae.execCtx.ExtractedItems)
			ae.execCtx.mu.Unlock()
			if err := ae.db.SaveExtractedData(ae.action.ID, ae.redactItems(items)); err != nil {
				ae.logger.Warn().Err(err).Msg("failed to save extracted data on success callback")
			}
		}
	}
}
