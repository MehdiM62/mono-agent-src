package task

import (
	"strings"
	"testing"
)

// --client-id makes a personal add idempotent; a session task cannot honour it,
// so it is refused instead of silently adding a duplicate on a retry.
func TestSessionAddRefusesClientID(t *testing.T) {
	t.Parallel()
	db := newTaskTestDB(t)
	s := sessionNew(t, db)
	doc := failedTaskJSON(t, db, "default", 3, "add", "x", "--session", id(s), "--acceptance", "y", "--client-id", "k1")
	if doc["code"] != "invalid_input" || !strings.Contains(doc["error"].(string), "--client-id") {
		t.Fatalf("a session task with --client-id: %v", doc)
	}
	var d sessionDoc
	mustTaskJSON(t, db, "default", &d, "", "session", "show", id(s))
	if d.Session.Counts.Total != 0 {
		t.Fatalf("a refused add left %d tasks", d.Session.Counts.Total)
	}
}
