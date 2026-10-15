package peoplenodes

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// people.save and people.sync_outlook_message act only on the execution's
// profile: a differing profile_id is refused and no profile is an error.
func TestPeopleNodesRequireExecutionProfile(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	SetGlobalPeopleDB(db)

	for _, node := range []workflow.NodeExecutor{&PeopleSaveNode{}, &SyncOutlookMessageNode{}} {
		withA := vault.ContextWithProfileID(context.Background(), "A")
		if _, err := node.Execute(withA, workflow.NodeInput{}, map[string]interface{}{"profile_id": "B"}); err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Errorf("%s: profile_id B under A: err = %v, want mismatch", node.Type(), err)
		}
		if _, err := node.Execute(context.Background(), workflow.NodeInput{}, map[string]interface{}{}); err == nil || !strings.Contains(err.Error(), node.Type()+" ran without a profile") {
			t.Errorf("%s: no profile: err = %v", node.Type(), err)
		}
	}
}
