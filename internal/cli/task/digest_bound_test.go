package task

import (
	"strings"
	"testing"
)

// A worker bound to a session or a task reads nothing of the personal board from the digest,
// neither the counts nor the next title, in text or in JSON.
func TestDigestOfABoundWorkerShowsNothingOfThePersonalBoard(t *testing.T) {
	db := newTaskTestDB(t)
	opsAdd(t, db, "private-title-xyz", "--ready")
	for _, env := range []map[string]string{
		{"MONOAGENT_TASK_SESSION": "5"},
		{"MONOAGENT_TASK_SESSION": "5", "MONOAGENT_TASK_ID": "9"},
		{"MONOAGENT_TASK_ID": "9"},
	} {
		for k, v := range env {
			setenv(t, k, v)
		}
		for _, jsonOut := range []bool{false, true} {
			out, _, err := runTask(t, db, "default", jsonOut, "", "digest")
			if err != nil {
				t.Fatalf("%v: %v", env, err)
			}
			if strings.Contains(out, "private-title-xyz") || strings.Contains(out, "1 ready") || strings.Contains(out, `"ready":1`) || strings.Contains(out, `"ready": 1`) {
				t.Errorf("%v json=%v: the digest shows the personal board: %q", env, jsonOut, out)
			}
		}
		for k := range env {
			setenv(t, k, "")
		}
	}
	// unbound, it still shows it
	out, _, err := runTask(t, db, "default", false, "", "digest")
	if err != nil || !strings.Contains(out, "private-title-xyz") {
		t.Fatalf("unbound digest: %q %v", out, err)
	}
}
