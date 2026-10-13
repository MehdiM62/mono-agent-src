package action

import (
	"sync"

	"github.com/monoes/mono-agent/internal/browser"
)

// ---------------------------------------------------------------------------
// Execution context
// ---------------------------------------------------------------------------

// ExecutionContext holds all mutable state for a single action execution run.
type ExecutionContext struct {
	mu              sync.Mutex
	Variables       map[string]interface{}
	StepResults     map[string]*StepResult
	Elements        map[string]browser.ElementHandle
	Data            map[string]interface{}
	ExtractedItems  []map[string]interface{}
	FailedItems     []FailedItem
	RecursionCounts map[string]int
	CurrentURL      string
	// ListOutput is set when the action produced records — rows from
	// extract_multiple, a list-returning bot method, or one result per loop
	// iteration — as opposed to per-step results about a single item (the
	// Gemini shape). The node layer emits records as separate items.
	ListOutput bool
	loopDepth  int
}

// NewExecutionContext returns an initialised execution context.
func NewExecutionContext() *ExecutionContext {
	return &ExecutionContext{
		Variables:       make(map[string]interface{}),
		StepResults:     make(map[string]*StepResult),
		Elements:        make(map[string]browser.ElementHandle),
		Data:            make(map[string]interface{}),
		ExtractedItems:  make([]map[string]interface{}, 0),
		FailedItems:     make([]FailedItem, 0),
		RecursionCounts: make(map[string]int),
	}
}

// SetVariable stores a variable in the context.
func (ec *ExecutionContext) SetVariable(name string, value interface{}) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.Variables[name] = value
}

// GetVariable retrieves a variable by name.
func (ec *ExecutionContext) GetVariable(name string) (interface{}, bool) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	v, ok := ec.Variables[name]
	return v, ok
}

// SetStepResult records the result of a step execution.
func (ec *ExecutionContext) SetStepResult(stepID string, result *StepResult) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.StepResults[stepID] = result
}

// GetStepResult retrieves the result of a previously executed step.
func (ec *ExecutionContext) GetStepResult(stepID string) *StepResult {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	return ec.StepResults[stepID]
}

// SetElement stores an ElementHandle reference by name.
func (ec *ExecutionContext) SetElement(name string, elem browser.ElementHandle) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.Elements[name] = elem
}

// GetElement retrieves a previously stored ElementHandle reference.
func (ec *ExecutionContext) GetElement(name string) browser.ElementHandle {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	return ec.Elements[name]
}

// SetData stores an arbitrary value in the data map.
func (ec *ExecutionContext) SetData(key string, value interface{}) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.Data[key] = value
}

// GetData retrieves a value from the data map.
func (ec *ExecutionContext) GetData(key string) (interface{}, bool) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	v, ok := ec.Data[key]
	return v, ok
}

// AddFailedItem appends a failure record.
func (ec *ExecutionContext) AddFailedItem(item FailedItem) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.FailedItems = append(ec.FailedItems, item)
}

// HasFailedItems reports whether any step recorded a failure via
// AddFailedItem (the "mark_failed" onError action, and its onFailure
// fallback) during this run.
func (ec *ExecutionContext) HasFailedItems() bool {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	return len(ec.FailedItems) > 0
}

// AddExtractedItem appends an extracted data record.
func (ec *ExecutionContext) AddExtractedItem(item map[string]interface{}) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.ExtractedItems = append(ec.ExtractedItems, item)
}

// AddRecord appends an item that is one record of a list result (see
// ListOutput).
func (ec *ExecutionContext) AddRecord(item map[string]interface{}) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.ExtractedItems = append(ec.ExtractedItems, item)
	ec.ListOutput = true
}

// inLoop reports whether a loop body is currently executing.
func (ec *ExecutionContext) inLoop() bool {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	return ec.loopDepth > 0
}

func (ec *ExecutionContext) enterLoop() {
	ec.mu.Lock()
	ec.loopDepth++
	ec.mu.Unlock()
}

func (ec *ExecutionContext) leaveLoop() {
	ec.mu.Lock()
	ec.loopDepth--
	ec.mu.Unlock()
}

// IncrementRecursion increments and returns the recursion counter for a key.
func (ec *ExecutionContext) IncrementRecursion(key string) int {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.RecursionCounts[key]++
	return ec.RecursionCounts[key]
}
