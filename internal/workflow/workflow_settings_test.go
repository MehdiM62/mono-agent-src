package workflow

import (
	"context"
	"testing"
)

func TestWorkflowFileStore_SettingsRoundTrip(t *testing.T) {
	store, err := NewWorkflowFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	wf := &Workflow{Name: "s", Settings: map[string]interface{}{"parallel_branches": true}}
	if err := store.SaveWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetWorkflow(ctx, wf.ID)
	if err != nil || loaded == nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if !parallelBranchesEnabled(loaded) {
		t.Errorf("settings lost on round trip: %v", loaded.Settings)
	}
}

func TestParallelBranchesEnabled(t *testing.T) {
	cases := []struct {
		settings map[string]interface{}
		want     bool
	}{
		{nil, false},
		{map[string]interface{}{"parallel_branches": false}, false},
		{map[string]interface{}{"parallel_branches": "yes"}, false},
		{map[string]interface{}{"parallel_branches": true}, true},
	}
	for _, c := range cases {
		if got := parallelBranchesEnabled(&Workflow{Settings: c.settings}); got != c.want {
			t.Errorf("settings %v: got %v, want %v", c.settings, got, c.want)
		}
	}
}
