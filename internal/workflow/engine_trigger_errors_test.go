package workflow

import (
	"context"
	"errors"
	"testing"
)

// The engine's trigger errors carry one prefix: callers already say
// "trigger workflow:", so the engine must not repeat it.
func TestTriggerWorkflow_ErrorPrefixOnce(t *testing.T) {
	store := newFullEngineStore(t)
	eng := newTestEngine(t, store, NewNodeTypeRegistry())
	ctx := context.Background()

	_, err := eng.TriggerWorkflow(ctx, "missing", nil)
	if !errors.Is(err, ErrWorkflowNotFound) || err.Error() != "engine: "+ErrWorkflowNotFound.Error() {
		t.Fatalf("missing workflow err = %v", err)
	}

	id := createActiveManualWorkflow(t, ctx, eng, "test.x")
	if err := store.SetWorkflowActive(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	_, err = eng.TriggerWorkflow(ctx, id, nil)
	if !errors.Is(err, ErrWorkflowInactive) || err.Error() != "engine: "+ErrWorkflowInactive.Error() {
		t.Fatalf("inactive workflow err = %v", err)
	}
}
