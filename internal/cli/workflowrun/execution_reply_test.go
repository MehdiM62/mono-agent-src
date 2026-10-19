//go:build !windows

package workflowrun

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/operator"
	"github.com/monoes/mono-agent/internal/workflow"
)

// A reply that lets a message to a person go out is answered by that person
// or the user: `workflow execution` from an agent, the chat's runs or a
// workflow's process leaves its links out; the user's own terminal shows
// them. A reply on the run's resume URL is left out even in front of nothing
// that sends, as that URL answers the run's other replies too.
func TestWorkflowExecutionHidesARepliesLinksFromAnAgent(t *testing.T) {
	cfg, db := seedExecutionDB(t)
	t.Setenv("MONOAGENT_PUBLIC_URL", "http://127.0.0.1:19721")
	if _, err := db.DB.Exec(`INSERT INTO workflow_executions (id, workflow_id, status, trigger_type, profile_id, created_at) VALUES
		('e-wait','w1','WAITING','trigger.manual','default','2026-09-26 10:00:00');
		INSERT INTO wait_replies (execution_id, node_id, run_index, workflow_id, token, kind, spec) VALUES
		('e-wait','ask',0,'w1','tokAsk','approval','{"kind":"approval","approval_type":"double","outbound":true}'),
		('e-wait','wait',0,'w1','tokWait','webhook','{"kind":"webhook","execution_url":true,"outbound":false}'),
		('e-wait','old',0,'w1','tokOld','approval','{"kind":"approval"}')`); err != nil {
		t.Fatal(err)
	}
	asOperator := func() {
		for _, k := range operator.MarkerEnv() {
			t.Setenv(k, "")
		}
	}
	detail := func() executionDetailJSON {
		t.Helper()
		cfg.JSONOutput = true
		out, err := runWorkflowCmdIn(t, cfg, "", "execution", "e-wait")
		if err != nil {
			t.Fatal(err)
		}
		var d executionDetailJSON
		if err := json.Unmarshal([]byte(out), &d); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		for _, tok := range []string{"tokAsk", "tokOld"} {
			if strings.Contains(out, tok) != (operator.Marker() == "") {
				t.Errorf("%s in the output: %v, operator: %v\n%s", tok, strings.Contains(out, tok), operator.Marker() == "", out)
			}
		}
		return d
	}

	for _, m := range [][2]string{{"CLAUDECODE", "1"}, {"MONOAGENT_AI_CALLER", "chat"}, {"MONOAGENT_WORKFLOW_RUN", "exec-9"}} {
		asOperator()
		t.Setenv(m[0], m[1])
		d := detail()
		if len(d.Waiting) != 3 || d.Waiting[0].URL != "" || d.Waiting[0].Links != nil || !d.Waiting[0].Outbound ||
			!strings.Contains(d.Waiting[0].Note, "message to a person") || d.Waiting[1].URL != "" || !d.Waiting[1].Shared ||
			!strings.Contains(d.Waiting[1].Note, "the run's resume URL") ||
			d.Waiting[2].URL != "" || !d.Waiting[2].Outbound {
			t.Errorf("%s set: waiting_for = %+v", m[0], d.Waiting)
		}
		cfg.JSONOutput = false
		out, err := runWorkflowCmdIn(t, cfg, "", "execution", "e-wait")
		if err != nil || strings.Contains(out, "tokAsk") || !strings.Contains(out, "Waiting at ask for a approval reply: answering it lets a message") {
			t.Errorf("%s set: text detail: %v\n%s", m[0], err, out)
		}
	}

	asOperator()
	d := detail()
	if len(d.Waiting) != 3 || d.Waiting[0].URL != "http://127.0.0.1:19721/reply/tokAsk" || len(d.Waiting[0].Links) != 2 || d.Waiting[0].Note != "" ||
		d.Waiting[2].URL != "http://127.0.0.1:19721/reply/tokOld" {
		t.Errorf("the user's terminal: waiting_for = %+v", d.Waiting)
	}
}

// An execution's resume URL answers every core.wait and form step of the
// run, so its token stays out of what a marked process sees even when the
// only reply open under it sends nothing: in the node data and the waiting
// list alike. A node's own reply that sends nothing keeps its token. The
// user's terminal shows both.
func TestWorkflowExecutionHidesARunsResumeTokenFromAnAgent(t *testing.T) {
	cfg, db := seedExecutionDB(t)
	t.Setenv("MONOAGENT_PUBLIC_URL", "http://127.0.0.1:19726")
	resume, err := workflow.ExecutionResumeURL("e-res")
	if err != nil {
		t.Fatal(err)
	}
	token := resume[strings.LastIndex(resume, "/")+1:]
	node := strings.Repeat("n", 43)
	if _, err := db.DB.Exec(`INSERT INTO workflow_executions (id, workflow_id, status, trigger_type, profile_id, created_at) VALUES
		('e-res','w1','WAITING','trigger.manual','default','2026-09-26 10:00:00')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO wait_replies (execution_id, node_id, run_index, workflow_id, token, kind, spec) VALUES
		('e-res','wait',0,'w1',?,'webhook','{"kind":"webhook","execution_url":true,"outbound":false}'),
		('e-res','ask',0,'w1',?,'approval','{"kind":"approval","outbound":false}')`, token, node); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO workflow_execution_nodes (id, execution_id, node_id, node_name, status, output_items, started_at)
		VALUES ('en1','e-res','set','Keep','SUCCESS',?,'2026-09-26 10:00:01')`, `[{"json":{"resume":"`+resume+`"}}]`); err != nil {
		t.Fatal(err)
	}
	show := func() string {
		t.Helper()
		out, err := runWorkflowCmdIn(t, cfg, "", "execution", "e-res")
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	for _, k := range operator.MarkerEnv() {
		t.Setenv(k, "")
	}
	if mine := show(); strings.Count(mine, token) != 2 || !strings.Contains(mine, node) {
		t.Errorf("the user's terminal:\n%s", mine)
	}
	t.Setenv("MONOAGENT_AI_CALLER", "chat")
	theirs := show()
	if strings.Contains(theirs, token) || !strings.Contains(theirs, `"resume": "http://127.0.0.1:19726/reply/`+workflow.ReplyTokenHidden+`"`) ||
		!strings.Contains(theirs, "the run's resume URL") || !strings.Contains(theirs, "http://127.0.0.1:19726/reply/"+node) {
		t.Errorf("the chat:\n%s", theirs)
	}
}
