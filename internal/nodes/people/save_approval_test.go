package peoplenodes

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/operator"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// A run the user did not start themselves never approves anyone for
// outreach; the user's own runs (a schedule, the app) save what they say
// (#831 S5).
func TestPeopleSaveApprovesOnlyInTheUsersRuns(t *testing.T) {
	for _, k := range operator.MarkerEnv() {
		t.Setenv(k, "")
	}
	db := setupTestDB(t)
	defer db.Close()
	SetGlobalPeopleDB(db)
	node := &PeopleSaveNode{}
	base := vault.ContextWithProfileID(context.Background(), "default")
	save := func(ctx context.Context, user string, fields map[string]interface{}) error {
		item := map[string]interface{}{"profile_url": "https://www.linkedin.com/in/" + user + "/", "platform": "linkedin"}
		for k, v := range fields {
			item[k] = v
		}
		_, err := node.Execute(ctx, workflow.NodeInput{Items: []workflow.Item{workflow.NewItem(item)}}, map[string]interface{}{})
		return err
	}
	row := func(user string) (category, intro string) {
		t.Helper()
		db.QueryRow(`SELECT COALESCE(category,''), COALESCE(introduction,'') FROM people WHERE platform_username = ?`, user).Scan(&category, &intro)
		return
	}

	aiRuns := map[string]context.Context{
		"an AI-authored workflow": workflow.WithAIAuthored(base),
		"a run the chat started":  workflow.WithTrigger(base, "manual", map[string]interface{}{workflow.AICallerKey: "chat"}),
	}
	for name, ctx := range aiRuns {
		err := save(ctx, "ada", map[string]interface{}{"category": "approved", "introduction": "Hi Ada"})
		if err == nil || !strings.Contains(err.Error(), "only the user approves") || !strings.Contains(err.Error(), "pending_approval") {
			t.Fatalf("%s saved approved: %v", name, err)
		}
		if cat, _ := row("ada"); cat != "" {
			t.Fatalf("%s: a refused save stored ada as %q", name, cat)
		}
	}
	// Queueing stays open to an AI run.
	if err := save(aiRuns["an AI-authored workflow"], "ada", map[string]interface{}{"category": "pending_approval", "introduction": "Hi Ada"}); err != nil {
		t.Fatal(err)
	}

	// The user's own run approves.
	if err := save(base, "bob", map[string]interface{}{"category": "approved", "introduction": "Hi Bob"}); err != nil {
		t.Fatalf("the user's run: %v", err)
	}
	if cat, _ := row("bob"); cat != "approved" {
		t.Fatalf("bob = %q", cat)
	}
	// A scheduled re-run of the user's scrape keeps bob approved.
	if err := save(base, "bob", map[string]interface{}{"full_name": "Bob B", "introduction": "Hi Bob, v2"}); err != nil {
		t.Fatal(err)
	}
	if cat, intro := row("bob"); cat != "approved" || intro != "Hi Bob, v2" {
		t.Fatalf("the user's re-run: %q %q", cat, intro)
	}
	// An AI run that re-saves bob as he is changes nothing; one that changes
	// his text puts him back in the queue.
	if err := save(aiRuns["a run the chat started"], "bob", map[string]interface{}{"full_name": "Bob B"}); err != nil {
		t.Fatal(err)
	}
	if cat, _ := row("bob"); cat != "approved" {
		t.Fatalf("an AI re-save changed bob to %q", cat)
	}
	if err := save(aiRuns["a run the chat started"], "bob", map[string]interface{}{"introduction": "Hi Bob, buy now"}); err != nil {
		t.Fatal(err)
	}
	if cat, intro := row("bob"); cat != "pending_approval" || intro != "Hi Bob, buy now" {
		t.Fatalf("an AI run's new text for bob: %q %q, want back in the queue", cat, intro)
	}
}
