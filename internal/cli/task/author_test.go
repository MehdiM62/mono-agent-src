package task

import (
	"strings"
	"testing"
)

// A task added from what is not the operator's own terminal (a chat's run, a
// workflow, an agent that gave no name) is that caller's, never "you": its
// history and task show name it, and reading the board is as before (C10).
func TestTasksAddedByWhatIsNotTheOperatorNameIt(t *testing.T) {
	t.Parallel()
	db := newTaskTestDB(t)
	mine := opsAdd(t, db, "mine")
	setenv(t, notOperatorEnv, "MONOAGENT_WORKFLOW_RUN is set, so a workflow run is running this command")
	setenv(t, addedByEnv, "workflow:exec-7")
	var added addedJSON
	mustTaskJSON(t, db, "default", &added, "", "add", "from", "a", "run")
	if added.Task.Author != "workflow:exec-7" || added.Task.Source.Kind != "agent" {
		t.Errorf("a workflow's task: %+v", added.Task)
	}
	if s := opsShow(t, db, added.Task.ID); s.Events[0].Actor != "workflow:exec-7" {
		t.Errorf("its history: %+v", s.Events)
	}
	if out, _, err := runTask(t, db, "default", false, "", "show", id(added.Task.ID)); err != nil || !strings.Contains(out, "Added by: workflow:exec-7") {
		t.Errorf("task show: %v\n%s", err, out)
	}
	if doc := failedTaskJSON(t, db, "default", 3, "add", "a capture", "--source", "os"); doc["code"] != "invalid_input" {
		t.Errorf("a workflow passing for the macOS menu: %v", doc)
	}
	var l listJSON
	mustTaskJSON(t, db, "default", &l, "", "list", "--status", "inbox")
	authors := map[int64]string{}
	for _, task := range l.Tasks {
		authors[task.ID] = task.Author
	}
	if len(authors) != 2 || authors[mine] != "you" || authors[added.Task.ID] != "workflow:exec-7" {
		t.Errorf("the Inbox read from the run: %+v", l.Tasks)
	}

	// An agent that gives no name is named by its kind; one that does, by its name.
	setenv(t, "CLAUDECODE", "1")
	setenv(t, addedByEnv, "agent:other")
	mustTaskJSON(t, db, "default", &added, "", "add", "unnamed")
	if added.Task.Author != "agent:other" {
		t.Errorf("an unnamed agent's task: %+v", added.Task)
	}
	mustTaskJSON(t, db, "default", &added, "", "add", "named", "--as", "bot")
	if added.Task.Author != "bot" {
		t.Errorf("a named agent's task: %+v", added.Task)
	}
	// A run marker no name can hold leaves the kind of caller.
	setenv(t, "CLAUDECODE", "")
	setenv(t, addedByEnv, "workflow:"+strings.Repeat("x", 80))
	mustTaskJSON(t, db, "default", &added, "", "add", "long")
	if added.Task.Author != "workflow" {
		t.Errorf("a long run marker: %+v", added.Task)
	}
	if s := opsShow(t, db, mine); s.Task.Status != "inbox" || s.Events[0].Actor != "you" {
		t.Errorf("the operator's task: %+v %+v", s.Task, s.Events)
	}
}
