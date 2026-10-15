package evaluation

import (
	"context"
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/profiledir"
	"github.com/monoes/mono-agent/internal/testdb"
)

// A store built with no profile refuses every call: there is no default
// profile to fall back on.
func TestEmptyProfileRefusesEveryMethod(t *testing.T) {
	ctx := context.Background()
	s := NewStore(testdb.Open(t).DB, "")
	calls := map[string]func() error{
		"CreateRun": func() error { _, err := s.CreateRun(ctx, "wf", nil); return err },
		"FinishRun": func() error { return s.FinishRun(ctx, &Run{ID: "r"}) },
		"AddCase":   func() error { return s.AddCase(ctx, "r", &Case{}) },
		"UpdateCase": func() error {
			return s.UpdateCase(ctx, &Case{ID: "c"}, false)
		},
		"LoadCase": func() error { return s.LoadCase(ctx, &Case{ID: "c"}) },
		"ListRuns": func() error { _, err := s.ListRuns(ctx, "wf"); return err },
		"GetRun":   func() error { _, err := s.GetRun(ctx, "r"); return err },
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, profiledir.ErrNoProfile) {
			t.Errorf("%s with no profile: %v, want ErrNoProfile", name, err)
		}
	}
}

// One profile's runs are invisible to another's.
func TestRunsAreIsolatedPerProfile(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t).DB
	a, b := NewStore(db, "A"), NewStore(db, "B")
	run, err := a.CreateRun(ctx, "wf", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := b.GetRun(ctx, run.ID); err != nil || r != nil {
		t.Errorf("profile B reads A's run: %v %v", r, err)
	}
	if runs, err := b.ListRuns(ctx, "wf"); err != nil || len(runs) != 0 {
		t.Errorf("profile B lists %v, %v", runs, err)
	}
}
