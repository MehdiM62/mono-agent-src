//go:build devaccount && !windows

package accountsmoke

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// executions is how many runs the CLI's database holds for a workflow.
func (r *rig) executions(workflowID string) int {
	r.t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(r.home, ".monoagent", "monoagent.db"))
	must(r.t, err)
	defer db.Close()
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM workflow_executions WHERE workflow_id = ?`, workflowID).Scan(&n)
	return n
}

// triggerWorkflow is a trigger node of the given type and config followed by a set node.
func triggerWorkflow(name, nodeType, config string) string {
	return fmt.Sprintf(`{"name":%q,"nodes":[
 {"id":"t","type":%q,"name":"Trigger","config":%s},
 {"id":"s","type":"core.set","name":"Set","config":{"assignments":"[{\"field\":\"ran\",\"value\":\"yes\"}]","include_input":false}}],
 "connections":[{"id":"c","source":"t","target":"s"}]}`, name, nodeType, config)
}

// A locked daemon arms no trigger: it opens no event stream (a queue consumer or a file watcher is
// armed the same way) and fires no schedule. A login arms them without a restart, and losing the
// session disarms them again: the stream is closed and the schedule stops.
func TestALockedDaemonArmsNoTrigger(t *testing.T) {
	var opened, open atomic.Int32
	stream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		opened.Add(1)
		open.Add(1)
		defer open.Add(-1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-req.Context().Done()
	}))
	t.Cleanup(stream.Close)

	r := newRig(t, rigOptions{enforce: past})
	r.signIn()
	r.workflow(triggerWorkflow("smoke-stream", "trigger.sse", fmt.Sprintf(`{"url":%q}`, stream.URL+"/events")), true)
	tick := r.workflow(triggerWorkflow("smoke-tick", "trigger.schedule", `{"cron":"*/2 * * * * *"}`), true)
	mustExit(t, r.run("account", "logout"), 0)

	d := r.startDaemon()
	time.Sleep(12 * time.Second) // two reconcile intervals; the schedule would have fired about six times
	if n := opened.Load(); n != 0 {
		t.Fatalf("a locked daemon opened the event stream %d times", n)
	}
	if n := r.executions(tick); n != 0 {
		t.Fatalf("a locked daemon ran the schedule %d times", n)
	}

	r.signIn()
	r.waitFor("a login to arm the stream", 60*time.Second, func() bool { return open.Load() == 1 })
	r.waitFor("a login to arm the schedule", 60*time.Second, func() bool { return r.executions(tick) > 0 })

	mustExit(t, r.run("account", "logout"), 0)
	r.waitFor("losing the session to close the stream", 60*time.Second, func() bool { return open.Load() == 0 })
	time.Sleep(3 * time.Second) // a run the last tick queued may still be finishing
	ran := r.executions(tick)
	time.Sleep(8 * time.Second)
	if n := r.executions(tick); n != ran {
		t.Fatalf("the schedule ran %d more times after the session was removed", n-ran)
	}
	if !d.alive() {
		t.Fatalf("the daemon died:\n%s", r.read("daemon.log"))
	}
}
