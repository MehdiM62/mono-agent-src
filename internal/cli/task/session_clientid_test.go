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

// A binding variable that is set but not a number is a broken worker environment, not a
// worker without one: the process must not act as if nothing bound it.
func TestSessionGarbledBindingEnvironmentFailsClosed(t *testing.T) {
	t.Parallel()
	db := newTaskTestDB(t)
	s := sessionNew(t, db)
	for _, env := range [][2]string{
		{"MONOAGENT_TASK_SESSION", "abc"},
		{"MONOAGENT_TASK_SESSION", "0"},
		{"MONOAGENT_TASK_SESSION", "1x"},
	} {
		setenv(t, env[0], env[1])
		if _, _, err := runTask(t, db, "default", true, "", "add", "x", "--session", id(s), "--acceptance", "y", "--as", "w"); err == nil {
			t.Errorf("%s=%s: an agent with a garbled binding added to a session", env[0], env[1])
		}
		setenv(t, env[0], "")
	}
}
