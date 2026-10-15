package workflow

import (
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/storage"
)

func TestOwnedBy(t *testing.T) {
	mine := &Workflow{ID: "w1", ProfileID: "a"}
	cases := []struct {
		name    string
		wf      *Workflow
		profile string
		want    bool
		wantErr error
	}{
		{"owner", mine, "a", true, nil},
		{"other profile", mine, "b", false, nil},
		{"no profile asked", mine, "", false, ErrNoProfile},
		{"ownerless workflow", &Workflow{ID: "w2"}, "a", false, ErrLegacyWorkflowNoProfile},
		{"ownerless workflow is not the default's", &Workflow{ID: "w2"}, "default", false, ErrLegacyWorkflowNoProfile},
		{"nil workflow", nil, "a", false, nil},
	}
	for _, c := range cases {
		got, err := OwnedBy(c.wf, c.profile)
		if got != c.want || !errors.Is(err, c.wantErr) {
			t.Errorf("%s: OwnedBy = %v, %v; want %v, %v", c.name, got, err, c.want, c.wantErr)
		}
	}
	if !errors.Is(ErrNoProfile, storage.ErrNoProfile) {
		t.Error("workflow.ErrNoProfile must be storage.ErrNoProfile")
	}
}
