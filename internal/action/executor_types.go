package action

import (
	"context"
	"time"

	"github.com/monoes/mono-agent/internal/browser"
)

// ---------------------------------------------------------------------------
// Step & loop definitions (deserialized from action JSON)
// ---------------------------------------------------------------------------

// StepDef represents a single step in an action definition.
type StepDef struct {
	ID            string                 `json:"id"`
	Type          string                 `json:"type"`
	URL           string                 `json:"url,omitempty"`
	Selector      string                 `json:"selector,omitempty"`
	XPath         string                 `json:"xpath,omitempty"`
	ConfigKey     string                 `json:"configKey,omitempty"`
	Alternatives  []string               `json:"alternatives,omitempty"`
	ElementRef    string                 `json:"elementRef,omitempty"`
	Value         interface{}            `json:"value,omitempty"`
	Text          string                 `json:"text,omitempty"`
	Attribute     string                 `json:"attribute,omitempty"`
	Direction     string                 `json:"direction,omitempty"`
	Duration      interface{}            `json:"duration,omitempty"`
	Timeout       float64                `json:"timeout,omitempty"`
	HumanLike     bool                   `json:"humanLike,omitempty"`
	MethodName    string                 `json:"methodName,omitempty"`
	Method        string                 `json:"method,omitempty"`
	Args          []interface{}          `json:"args,omitempty"`
	Variable      string                 `json:"variable,omitempty"`
	VariableName  string                 `json:"variable_name,omitempty"`
	WaitFor       string                 `json:"waitFor,omitempty"`
	WaitAfter     string                 `json:"waitAfter,omitempty"`
	RaceSelectors map[string]string      `json:"raceSelectors,omitempty"`
	Condition     interface{}            `json:"condition,omitempty"`
	Then          []string               `json:"then,omitempty"`
	Else          []string               `json:"else,omitempty"`
	OnError       *ErrorHandlerDef       `json:"onError,omitempty"`
	OnSuccess     *SuccessAction         `json:"onSuccess,omitempty"`
	Set           map[string]interface{} `json:"set,omitempty"`
	Description   string                 `json:"description,omitempty"`
	DataSource    string                 `json:"dataSource,omitempty"`
	BatchSize     int                    `json:"batchSize,omitempty"`
	Increment     string                 `json:"increment,omitempty"`
	// Intent describes the step's target element in plain words (e.g. "the
	// chat's Send button"). It is only used by the opt-in Jev fallback
	// (jevfallback.go) when the step's selector/xpath finds nothing; a step
	// without an intent never falls back.
	Intent string `json:"intent,omitempty"`

	// --- package-era fields (spec §4.3, §6.1) ---

	// SideEffect marks a step that writes, sends or deletes on the site.
	// Safe-mode verification (SetSafeMode) stops before the first one.
	SideEffect bool `json:"sideEffect,omitempty"`
	// Until is a post-step outcome to wait for (click/type/submit), and
	// the condition of a wait_for step.
	Until *WaitSpec `json:"until,omitempty"`
	// Fragment names fragments/<name>.json for call_fragment.
	Fragment string `json:"fragment,omitempty"`
	// Action is the call_action reference ("<action>" or "<automation>.<action>").
	Action string `json:"action,omitempty"`
	// Inputs are the variables passed to call_fragment / call_action /
	// page_script (values are templates).
	Inputs map[string]interface{} `json:"inputs,omitempty"`
	// Items is the for_each list template; As names the item variable
	// (default "item"); Steps is the inline loop body.
	Items string    `json:"items,omitempty"`
	As    string    `json:"as,omitempty"`
	Steps []StepDef `json:"steps,omitempty"`
	// Key is the key for press_key ("Enter", "Escape", "Control+a", ...).
	Key string `json:"key,omitempty"`
	// Script names scripts/<name>.js for page_script.
	Script string `json:"script,omitempty"`
	// Input is the source template for transform; Ops its operations.
	Input string        `json:"input,omitempty"`
	Ops   []TransformOp `json:"ops,omitempty"`
	// Fields maps output field → sub-selector for extract_table /
	// extract_list-style steps (relative to each row/item).
	Fields map[string]string `json:"fields,omitempty"`
	// Path is a JSON path ("a.b[0].c") for extract_json.
	Path string `json:"path,omitempty"`
}

// LoopDef defines an iteration over a collection of items, executing a subset
// of the action's steps for each item.
//
// NOTE: a legacy "onComplete" field appears in some shipped loop JSONs. It is
// deliberately NOT modeled here — it was historically parsed but never
// executed, so json.Unmarshal now silently ignores it for JSON compatibility.
type LoopDef struct {
	ID       string   `json:"id"`
	Iterator string   `json:"iterator"`
	IndexVar string   `json:"indexVar"`
	Steps    []string `json:"steps"`
	MaxItems string   `json:"maxItems,omitempty"`
	// MaxItemsPerDay is an optional template (e.g. "{{maxFollowsPerDay or
	// 150}}") resolving to a cross-run daily cap, persisted in the
	// action_daily_counters table and keyed by (profile, action type, UTC
	// day). Unlike MaxItems (a per-session, in-memory cap), this cap
	// survives across separate action runs on the same day.
	MaxItemsPerDay string `json:"maxItemsPerDay,omitempty"`
}

// ConditionDef describes a conditional expression for condition steps.
type ConditionDef struct {
	Variable string      `json:"variable"`
	Operator string      `json:"operator"` // "exists", "not_exists", "equals", "not_equals", "greater_than", "contains"
	Value    interface{} `json:"value,omitempty"`
}

// SuccessAction defines what happens when a step succeeds.
type SuccessAction struct {
	Action    string      `json:"action"` // "set_variable", "increment", "save_data", "update_progress"
	Variable  string      `json:"variable,omitempty"`
	Value     interface{} `json:"value,omitempty"`
	Increment string      `json:"increment,omitempty"`
}

// ---------------------------------------------------------------------------
// Event types
// ---------------------------------------------------------------------------

// ExecutionEvent is emitted during execution to allow external monitoring.
type ExecutionEvent struct {
	Type     string `json:"type"`
	ActionID string `json:"actionId"`
	StepID   string `json:"stepId,omitempty"`
	Index    int    `json:"index,omitempty"`
	Total    int    `json:"total,omitempty"`
	Message  string `json:"message,omitempty"`
}

// ---------------------------------------------------------------------------
// Result types
// ---------------------------------------------------------------------------

// FailedItem records a step failure for later reporting.
type FailedItem struct {
	StepID    string
	Error     error
	Timestamp time.Time
	// Index is the loop item the failure happened on; when Target is set it
	// is the failed item's index in the run's target list (selectedListItems).
	Index  int
	Target bool
}

// StepResult captures the outcome of a single step execution.
type StepResult struct {
	Success bool
	Element browser.ElementHandle
	Data    interface{}
	Error   error
	Abort   bool
	Skip    bool
	Retry   bool
	StepID  string
}

// ExecutionResult is the aggregate outcome of an entire action execution.
type ExecutionResult struct {
	ExtractedItems []map[string]interface{}
	// ListOutput: ExtractedItems are separate records (see
	// ExecutionContext.ListOutput), not facets of one item.
	ListOutput  bool
	FailedItems []FailedItem
	// SucceededTargets are the indexes, in the run's target list, of the
	// targets whose iteration ran to its end without a failure recorded.
	SucceededTargets []int
	// ExtractedTargets[i] is the target ExtractedItems[i] was produced for,
	// -1 when it came from outside a target iteration.
	ExtractedTargets []int
	TotalProcessed   int
	Duration         time.Duration
}

// ---------------------------------------------------------------------------
// Interfaces (to avoid circular imports with storage/config packages)
// ---------------------------------------------------------------------------

// StorageInterface abstracts the database operations needed by the executor.
type StorageInterface interface {
	UpdateActionState(id, state string) error
	UpdateActionReachedIndex(id string, index int) error
	SaveExtractedData(actionID string, items []map[string]interface{}) error
	// GetDailyActionCount returns how many times actionType has already
	// been recorded today (UTC, see action_daily_counters) for whatever
	// profile this StorageInterface implementation is scoped to.
	GetDailyActionCount(actionType string) (int, error)
	// IncrementDailyActionCount records one more occurrence of actionType
	// today (UTC) and returns the count after incrementing.
	IncrementDailyActionCount(actionType string) (int, error)
}

// ConfigInterface abstracts the configuration manager.
type ConfigInterface interface {
	GetConfig(social, action, configContext, htmlContent, purpose string, schema map[string]interface{}) (interface{}, error)
}

// BotAdapter is the interface for platform-specific bot methods that can be
// called via the "call_bot_method" step type.
type BotAdapter interface {
	// GetMethodByName returns a callable function for the given method name.
	// The returned function accepts a context and variadic args, returning
	// (result interface{}, err error).
	GetMethodByName(name string) (func(ctx context.Context, args ...interface{}) (interface{}, error), bool)
}

// StorageAction is a flat representation of an action from the storage layer,
// used to avoid a direct dependency on the storage package.
type StorageAction struct {
	ID                string
	CreatedAt         int64
	Title             string
	Type              string
	State             string
	TargetPlatform    string
	ContentSubject    string
	ContentMessage    string
	ContentBlobURLs   string
	Keywords          string
	ReachedIndex      int
	ScheduledDate     string
	StartDate         string
	EndDate           string
	ExecutionInterval int
	CampaignID        string
	Params            map[string]interface{}
}

// ---------------------------------------------------------------------------
// Step handler signature
// ---------------------------------------------------------------------------

// StepHandler is the function signature for individual step implementations.
type StepHandler func(ctx context.Context, step StepDef) (*StepResult, error)
