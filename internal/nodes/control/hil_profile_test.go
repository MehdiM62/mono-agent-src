package control

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/vault"
)

func TestHIL_RequiresExecutionProfile(t *testing.T) {
	db := newHILTestDB(t)
	ctx := vault.ContextWithDB(context.Background(), db)
	n := &HumanInLoopNode{}

	if _, err := n.Execute(ctx, hilInput(), map[string]interface{}{}); err == nil || !strings.Contains(err.Error(), "core.human_in_loop ran without a profile") {
		t.Fatalf("no profile: err = %v", err)
	}
	withA := vault.ContextWithProfileID(ctx, "A")
	if _, err := n.Execute(withA, hilInput(), map[string]interface{}{"profile_id": "B"}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched profile_id: err = %v", err)
	}
}
