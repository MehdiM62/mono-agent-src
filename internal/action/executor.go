package action

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/browser"
	"github.com/monoes/mono-agent/internal/jev"
	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// ActionExecutor
// ---------------------------------------------------------------------------

// ActionExecutor orchestrates the execution of a loaded action definition
// against a browser page.
type ActionExecutor struct {
	// SuccessfulStep observes confirmed steps without changing execution or retry behavior.
	SuccessfulStep func(StepDef, *StepResult)

	ctx          context.Context
	page         browser.PageInterface
	db           StorageInterface
	configMgr    ConfigInterface
	events       chan<- ExecutionEvent
	botAdapter   BotAdapter
	logger       zerolog.Logger
	execCtx      *ExecutionContext
	resolver     *VariableResolver
	errorHandler *ErrorHandler
	handlers     map[string]StepHandler
	action       *StorageAction
	actionDef    *ActionDef
	startTime    time.Time
	// reachedIndexByLoop tracks per-loop progress within a single Execute
	// run so that multiple loops (or a re-triggered loop) do not share one
	// reached index.
	reachedIndexByLoop map[string]int
	// Jev element-picker fallback (jevfallback.go); nil client = off.
	jevClient *jev.Client
	jevMinP   float64
	jevMarker string // marker set by the current step's pick, if any

	// Package-era state (package_ctx.go). pkg is nil for legacy actions.
	pkg      PackageContext
	selObs   SelectorObserver
	safeMode bool
	// safeStop is set when safe mode stopped before a side-effect step.
	safeStop *SafeStop
	// fatal is set by failRun (safe_mode.go): the run must unwind.
	fatal            error
	downloadsAllowed bool
	// selMarkers are in-page marker tokens set by package-selector lookups
	// (selectors_pkg.go) during the current step; removed when it ends.
	selMarkers []string
}

// NewActionExecutor creates a fully initialised executor. The page must already
// be navigated to the platform's domain (or will be by the first navigate
// step). events may be nil if no external monitoring is needed.
func NewActionExecutor(
	ctx context.Context,
	page browser.PageInterface,
	db StorageInterface,
	configMgr ConfigInterface,
	events chan<- ExecutionEvent,
	botAdapter BotAdapter,
	logger zerolog.Logger,
) *ActionExecutor {
	execCtx := NewExecutionContext()
	ae := &ActionExecutor{
		ctx:                ctx,
		page:               page,
		db:                 db,
		configMgr:          configMgr,
		events:             events,
		botAdapter:         botAdapter,
		logger:             logger.With().Str("component", "executor").Logger(),
		execCtx:            execCtx,
		resolver:           NewVariableResolver(execCtx),
		errorHandler:       NewErrorHandler(),
		handlers:           make(map[string]StepHandler),
		reachedIndexByLoop: make(map[string]int),
	}
	ae.resolver.scope = ae.secretScope
	ae.resolver.declaredSecret = func(name string) bool { return ae.secretInputNames()[name] }
	ae.initHandlers()
	return ae
}

// SetJevPicker enables the Jev element-picker fallback: a step with an
// intent whose selector/xpath finds nothing asks Jev for the matching
// element, accepted only when its top probability is ≥ minP. A nil client
// disables it (the default).
func (ae *ActionExecutor) SetJevPicker(c *jev.Client, minP float64) {
	ae.jevClient = c
	ae.jevMinP = minP
}

// SetVariable pre-seeds a variable in the execution context before Execute() is
// called. Use this to pass user-supplied data (e.g., selectedListItems) into the
// action pipeline.
func (ae *ActionExecutor) SetVariable(key string, value interface{}) {
	ae.execCtx.SetVariable(key, value)
}

// initHandlers registers all step type handlers in the dispatch map.
func (ae *ActionExecutor) initHandlers() {
	ae.handlers["navigate"] = ae.stepNavigate
	ae.handlers["wait"] = ae.stepWait
	ae.handlers["refresh"] = ae.stepRefresh
	ae.handlers["find_element"] = ae.stepFindElement
	ae.handlers["click"] = ae.stepClick
	ae.handlers["type"] = ae.stepType
	ae.handlers["upload"] = ae.stepUpload
	ae.handlers["scroll"] = ae.stepScroll
	ae.handlers["hover"] = ae.stepHover
	ae.handlers["extract_text"] = ae.stepExtractText
	ae.handlers["extract_attribute"] = ae.stepExtractAttribute
	ae.handlers["extract_multiple"] = ae.stepExtractMultiple
	ae.handlers["condition"] = ae.stepCondition
	ae.handlers["update_progress"] = ae.stepUpdateProgress
	ae.handlers["save_data"] = ae.stepSaveData
	ae.handlers["mark_failed"] = ae.stepMarkFailed
	ae.handlers["log"] = ae.stepLog
	ae.handlers["call_bot_method"] = ae.stepCallBotMethod
	ae.handlers["set_variable"] = ae.stepSetVariable
	ae.initExtendedHandlers()
}

// Execute runs the complete action. It follows four phases:
//  1. Identify which steps belong to loops
//  2. Execute initial (non-loop) steps
//  3. Execute each loop
//  4. Aggregate and return results
func (ae *ActionExecutor) Execute(action *StorageAction) (*ExecutionResult, error) {
	actionDef, err := GetLoader().Load(action.TargetPlatform, action.Type)
	if err != nil {
		return nil, fmt.Errorf("loading action definition: %w", err)
	}
	return ae.ExecuteDef(action, actionDef)
}

// ExecuteDef runs an already-loaded definition (e.g. a recording draft that
// is not installed). The definition is validated first and refused on
// errors. In safe mode a run that stops before a side-effect step returns
// success; SafeStopped reports where.
func (ae *ActionExecutor) ExecuteDef(action *StorageAction, actionDef *ActionDef) (*ExecutionResult, error) {
	res, err := ae.executeDef(action, actionDef)
	return res, ae.redactErr(err)
}

func (ae *ActionExecutor) executeDef(action *StorageAction, actionDef *ActionDef) (*ExecutionResult, error) {
	// A locked monoes.me account drives no browser (spec section 6.2): node run,
	// login, crawl, capture, apply and every other direct action path end here.
	if lrErr := account.Require(ae.ctx); lrErr != nil {
		return nil, lrErr
	}
	ae.startTime = time.Now()
	ae.action = action
	ae.actionDef = actionDef

	if err := ae.prepareRun(action.TargetPlatform, actionDef); err != nil {
		return nil, err
	}
	actionDef = ae.actionDef // prepareRun may assign synthetic step ids

	// Seed the execution context with action fields.
	ae.seedVariables(action)
	ae.registerSecretInputs()

	// Validate declared required inputs before executing anything.
	if err := ae.validateRequiredInputs(actionDef); err != nil {
		ae.logger.Error().Err(err).Msg("required input validation failed")
		return nil, err
	}

	ae.emitEvent(ExecutionEvent{
		Type:     "action_start",
		ActionID: action.ID,
		Message:  fmt.Sprintf("Starting %s/%s", action.TargetPlatform, action.Type),
	})

	// Update state to RUNNING.
	if ae.db != nil {
		if err := ae.db.UpdateActionState(action.ID, "RUNNING"); err != nil {
			ae.logger.Warn().Err(err).Msg("failed to update action state to RUNNING")
		}
	}

	// Phase 1: Identify all step IDs that are referenced by loops.
	loopStepIDs := make(map[string]bool)
	for _, loop := range actionDef.Loops {
		for _, id := range ae.getAllReferencedIDs(loop, actionDef.Steps) {
			loopStepIDs[id] = true
		}
	}

	// Identify all step IDs referenced only in condition then/else branches.
	// These should not be included in initialSteps — the condition step executes
	// them directly via executeSteps when the branch is taken.
	conditionBranchIDs := make(map[string]bool)
	var collectBranches func(steps []StepDef)
	collectBranches = func(steps []StepDef) {
		for _, step := range steps {
			for _, id := range step.Then {
				conditionBranchIDs[id] = true
			}
			for _, id := range step.Else {
				conditionBranchIDs[id] = true
			}
			// A condition inside a for_each body may branch to top-level
			// steps too; those must not also run as initial steps.
			collectBranches(step.Steps)
		}
	}
	collectBranches(actionDef.Steps)

	// Phase 2: Execute initial (non-loop, non-condition-branch) steps in order.
	var initialSteps []StepDef
	for _, step := range actionDef.Steps {
		if !loopStepIDs[step.ID] && !conditionBranchIDs[step.ID] {
			initialSteps = append(initialSteps, step)
		}
	}

	if err := ae.executeSteps(ae.ctx, initialSteps); err != nil && !errors.Is(err, errSafeStop) {
		if errors.Is(err, ErrAbort) {
			ae.logger.Error().Msg("action aborted during initial steps")
			if ae.db != nil {
				if serr := ae.db.UpdateActionState(action.ID, "FAILED"); serr != nil {
					ae.logger.Error().Err(serr).Str("actionID", action.ID).Msg("failed to persist action state FAILED — action may appear stuck")
				}
			}
			return ae.buildResult(), err
		}
		if isContextError(err) {
			ae.logger.Error().Err(err).Msg("action cancelled/timed out during initial steps")
			if ae.db != nil {
				if serr := ae.db.UpdateActionState(action.ID, "FAILED"); serr != nil {
					ae.logger.Error().Err(serr).Str("actionID", action.ID).Msg("failed to persist action state FAILED — action may appear stuck")
				}
			}
			return ae.buildResult(), err
		}
		ae.logger.Warn().Err(err).Msg("error during initial steps (continuing to loops)")
	}

	// Phase 3: Execute loops.
	for _, loop := range actionDef.Loops {
		if ae.safeStop != nil {
			break
		}
		if err := ae.executeLoop(ae.ctx, loop, actionDef.Steps); err != nil && !errors.Is(err, errSafeStop) {
			if errors.Is(err, ErrAbort) {
				ae.logger.Error().Str("loopID", loop.ID).Msg("action aborted during loop")
				if ae.db != nil {
					if serr := ae.db.UpdateActionState(action.ID, "FAILED"); serr != nil {
						ae.logger.Error().Err(serr).Str("actionID", action.ID).Msg("failed to persist action state FAILED — action may appear stuck")
					}
				}
				return ae.buildResult(), err
			}
			if isContextError(err) {
				ae.logger.Error().Err(err).Str("loopID", loop.ID).Msg("action cancelled/timed out during loop")
				if ae.db != nil {
					if serr := ae.db.UpdateActionState(action.ID, "FAILED"); serr != nil {
						ae.logger.Error().Err(serr).Str("actionID", action.ID).Msg("failed to persist action state FAILED — action may appear stuck")
					}
				}
				return ae.buildResult(), err
			}
			ae.logger.Warn().Err(err).Str("loopID", loop.ID).Msg("loop completed with errors")
		}
	}

	// Phase 4: Determine the final state honestly. Individual steps that hit
	// the "mark_failed" onError action (or exhaust retries with no other
	// onFailure policy — see errors.go's handleOnFailure default) don't abort
	// the run — by design, later steps and loops still execute — but they DO
	// record a FailedItem. Reaching here with any FailedItems recorded means
	// the action ran to completion while failing to do the thing it was
	// actually asked to do (e.g. Gemini's generate_image action: every
	// fallback tier for typing/sending/waiting failed), so it must be
	// reported as FAILED, not COMPLETED — previously this branch always
	// reported COMPLETED and returned a nil error regardless of FailedItems,
	// which is why a workflow node wrapping this executor (BrowserNode.Execute)
	// could show green/success even though nothing it was asked to do worked.
	if ae.execCtx.HasFailedItems() {
		result := ae.buildResult()
		if ae.db != nil {
			if serr := ae.db.UpdateActionState(action.ID, "FAILED"); serr != nil {
				ae.logger.Error().Err(serr).Str("actionID", action.ID).Msg("failed to persist action state FAILED — action may appear stuck RUNNING")
			}
		}
		summary := result.FailedItems[len(result.FailedItems)-1]
		ae.emitEvent(ExecutionEvent{
			Type:     "action_failed",
			ActionID: action.ID,
			Message:  fmt.Sprintf("%s/%s failed: %d step(s) failed, last: %s: %v", action.TargetPlatform, action.Type, len(result.FailedItems), summary.StepID, summary.Error),
		})
		return result, fmt.Errorf("action %s/%s: %d step(s) failed, last: %s: %w", action.TargetPlatform, action.Type, len(result.FailedItems), summary.StepID, summary.Error)
	}

	if ae.db != nil {
		if serr := ae.db.UpdateActionState(action.ID, "COMPLETED"); serr != nil {
			ae.logger.Error().Err(serr).Str("actionID", action.ID).Msg("failed to persist action state COMPLETED — action may appear stuck RUNNING")
		}
	}

	ae.emitEvent(ExecutionEvent{
		Type:     "action_complete",
		ActionID: action.ID,
		Message:  fmt.Sprintf("Completed %s/%s", action.TargetPlatform, action.Type),
	})

	return ae.buildResult(), nil
}

// isContextError reports whether err is a context cancellation or deadline
// exhaustion (possibly wrapped), used to persist an honest FAILED state
// instead of COMPLETED for cancelled/timed-out runs.
func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// emitEvent sends an execution event to the events channel. If the channel is
// nil or full, the event is silently dropped.
func (ae *ActionExecutor) emitEvent(event ExecutionEvent) {
	if ae.events == nil {
		return
	}
	event.Message = ae.redact(event.Message)
	select {
	case ae.events <- event:
	default:
		ae.logger.Debug().
			Str("type", event.Type).
			Str("stepID", event.StepID).
			Msg("event channel full, dropping event")
	}
}

// buildResult aggregates the execution context into a final ExecutionResult.
func (ae *ActionExecutor) buildResult() *ExecutionResult {
	ae.execCtx.mu.Lock()
	defer ae.execCtx.mu.Unlock()

	extracted := ae.redactItems(ae.execCtx.ExtractedItems)
	if extracted == nil {
		extracted = []map[string]interface{}{}
	}

	failed := make([]FailedItem, len(ae.execCtx.FailedItems))
	copy(failed, ae.execCtx.FailedItems)
	for i := range failed {
		failed[i].Error = ae.redactErr(failed[i].Error)
	}

	return &ExecutionResult{
		ExtractedItems: extracted,
		ListOutput:     ae.execCtx.ListOutput,
		FailedItems:    failed,
		TotalProcessed: len(extracted) + len(failed),
		Duration:       time.Since(ae.startTime),
	}
}
