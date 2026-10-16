package workflowrun

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/operator"
)

// A reply that lets a message to a person go out is answered by that person
// or the user: `workflow execution` from an agent, the chat's runs or a
// workflow's process leaves its links out; the user's own terminal shows
// them, and a reply in front of nothing that sends keeps its link.
func TestWorkflowExecutionHidesARepliesLinksFromAnAgent(t *testing.T) {
	cfg, db := seedExecutionDB(t)
	t.Setenv("MONOAGENT_PUBLIC_URL", "http://127.0.0.1:19721")
	if _, err := db.DB.Exec(`INSERT INTO workflow_executions (id, workflow_id, status, trigger_type, profile_id, created_at) VALUES
		('e-wait','w1','WAITING','trigger.manual','default','2026-09-26 10:00:00');
		INSERT INTO wait_replies (execution_id, node_id, run_index, workflow_id, token, kind, spec) VALUES
		('e-wait','ask',0,'w1','tokAsk','approval','{"kind":"approval","approval_type":"double","outbound":true}'),
		('e-wait','wait',0,'w1','tokWait','webhook','{"kind":"webhook","execution_url":true}')`); err != nil {
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
		if strings.Contains(out, "tokAsk") != (operator.Marker() == "") {
			t.Errorf("the send's reply token in the output: %v, operator: %v\n%s", strings.Contains(out, "tokAsk"), operator.Marker() == "", out)
		}
		return d
	}

	for _, m := range [][2]string{{"CLAUDECODE", "1"}, {"MONOAGENT_AI_CALLER", "chat"}, {"MONOAGENT_WORKFLOW_RUN", "exec-9"}} {
		asOperator()
		t.Setenv(m[0], m[1])
		d := detail()
		if len(d.Waiting) != 2 || d.Waiting[0].URL != "" || d.Waiting[0].Links != nil || !d.Waiting[0].Outbound ||
			!strings.Contains(d.Waiting[0].Note, "message to a person") || d.Waiting[1].URL != "http://127.0.0.1:19721/reply/tokWait" {
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
	if len(d.Waiting) != 2 || d.Waiting[0].URL != "http://127.0.0.1:19721/reply/tokAsk" || len(d.Waiting[0].Links) != 2 || d.Waiting[0].Note != "" {
		t.Errorf("the user's terminal: waiting_for = %+v", d.Waiting)
	}
}
