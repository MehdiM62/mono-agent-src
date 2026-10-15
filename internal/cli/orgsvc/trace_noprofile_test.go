package orgsvc

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/storage"
)

// A trace for a workflow that does not exist is an error: its profile is never guessed.
func TestAdmitWebhookTraceWithoutAWorkflowIsAnError(t *testing.T) {
	f := newOrgCLIFixture(t)
	db, err := storage.NewDatabase(f.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Services{db: db, openWorkflowStore: openTestWorkflowStore, logf: t.Logf}
	if _, _, err := s.admitWebhookTrace(context.Background(), "wf-missing", "chn_x", 0); err == nil {
		t.Fatal("a trace for an unknown workflow was admitted under a guessed profile")
	}
}
