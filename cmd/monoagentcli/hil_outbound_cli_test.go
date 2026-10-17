package main

import (
	"testing"

	"github.com/monoes/mono-agent/internal/commsync"
	"github.com/monoes/mono-agent/internal/storage"
)

// A workflow Human-in-Loop item whose approval lets a message to a person go
// out is the user's: `hil approve|reject` from an agent, the chat's runs or a
// workflow's process exit 3 and leave it pending, an old item (no mark, a
// Communications row) included. A plain workflow item is still theirs to
// decide, and the operator decides both.
func TestHILOutboundItemIsTheOperators(t *testing.T) {
	cfg := sendEnv(t)
	db, err := storage.NewDatabase(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	add := func(id, nc string) {
		if _, err := db.DB.Exec(`INSERT INTO hil_pending (id, execution_id, workflow_id, node_id, node_name, status, node_config, profile_id)
			VALUES (?, 'ex-1', 'wf-1', 'n', 'Review', 'pending', ?, 'default')`, id, nc); err != nil {
			t.Fatal(err)
		}
	}
	status := func(id string) string {
		var st string
		if err := db.DB.QueryRow(`SELECT status FROM hil_pending WHERE id = ?`, id).Scan(&st); err != nil {
			t.Fatal(err)
		}
		return st
	}
	add("out-1", `{"outbound":true}`)
	add("out-2", `{"outbound":true}`)
	add("old-1", `{}`)
	add("old-2", `{"readonly_fields":null}`) // queued before the decision was recorded, no Communications row
	if _, err := commsync.Queue(t.Context(), db.DB, commsync.Item{HilRef: "old-1", Profile: "default", Platform: "email", Action: "email",
		Recipient: commsync.Recipient{Name: "Ada", Handle: "ada@example.test"}, Text: "Hi Ada"}); err != nil {
		t.Fatal(err)
	}

	for _, m := range [][2]string{{"CLAUDECODE", "1"}, {"MONOAGENT_AI_CALLER", "chat"}, {"MONOAGENT_WORKFLOW_RUN", "exec-9"}} {
		asOperatorProcess(t)
		t.Setenv(m[0], m[1])
		for _, verb := range []string{"approve", "reject"} {
			for _, id := range []string{"out-1", "old-1", "old-2"} {
				if _, err := runHIL(t, cfg, verb, id); exitCodeFor(err) != 3 || status(id) != "pending" {
					t.Errorf("%s set: hil %s %s: %v (exit %d), left %s", m[0], verb, id, err, exitCodeFor(err), status(id))
				}
			}
		}
		plain := "plain-" + m[0]
		add(plain, `{"outbound":false}`)
		if _, err := runHIL(t, cfg, "approve", plain, "--data", `{"x":1}`); err != nil || status(plain) != "approved" {
			t.Errorf("%s set: a plain workflow item: %v, %s", m[0], err, status(plain))
		}
	}

	asOperatorProcess(t)
	if _, err := runHIL(t, cfg, "approve", "out-1"); err != nil || status("out-1") != "approved" {
		t.Fatalf("operator approve: %v, %s", err, status("out-1"))
	}
	if _, err := runHIL(t, cfg, "reject", "out-2"); err != nil || status("out-2") != "rejected" {
		t.Fatalf("operator reject: %v, %s", err, status("out-2"))
	}
}
