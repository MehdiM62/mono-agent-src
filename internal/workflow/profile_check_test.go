package workflow

import (
	"errors"
	"testing"
)

// A workflow with no profile id is an error state for a single-profile engine
// (neither the default profile's nor everyone's); only the daemon engine,
// which spans profiles, lets the check through.
func TestCheckWorkflowProfileUnownedIsAnError(t *testing.T) {
	work := &WorkflowEngine{profileID: "p-work"}
	if err := work.checkWorkflowProfile(&Workflow{ID: "w"}); !errors.Is(err, ErrLegacyWorkflowNoProfile) {
		t.Fatalf("unowned workflow: err = %v, want ErrLegacyWorkflowNoProfile", err)
	}
	if err := work.checkWorkflowProfile(&Workflow{ID: "w", ProfileID: "p-work"}); err != nil {
		t.Fatalf("own workflow refused: %v", err)
	}
	if err := work.checkWorkflowProfile(&Workflow{ID: "w", ProfileID: "p-other"}); err == nil {
		t.Fatal("p-work ran another profile's workflow")
	}
	def := &WorkflowEngine{profileID: "default"}
	if err := def.checkWorkflowProfile(&Workflow{ID: "w"}); !errors.Is(err, ErrLegacyWorkflowNoProfile) {
		t.Fatalf("default must not own an ownerless workflow: %v", err)
	}
	none := &WorkflowEngine{}
	if err := none.checkWorkflowProfile(&Workflow{ID: "w", ProfileID: "p-work"}); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("an engine with no profile: err = %v, want ErrNoProfile", err)
	}
	all := &WorkflowEngine{allowAllProfiles: true}
	if err := all.checkWorkflowProfile(&Workflow{ID: "w", ProfileID: "p-work"}); err != nil {
		t.Fatalf("the daemon runs every profile: %v", err)
	}
}
