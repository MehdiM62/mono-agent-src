package org

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/monoes/mono-agent/internal/daemonhb"
	"github.com/monoes/mono-agent/internal/orgdecide"
	"github.com/monoes/mono-agent/internal/orgdesign"
	"github.com/monoes/mono-agent/internal/storage"
)

// With the daemon running and the org at autonomy full, `needs-you` lists
// a Human-in-Loop item in front of a message to a person: only a person
// decides it, whatever the level (#831 round 7). One in front of no send
// is the decider's, as before.
func TestNeedsYouListsItemsThatSendAMessageAtFull(t *testing.T) {
	f := newOrgCLIFixture(t)
	bin := filepath.Join(t.TempDir(), "monomind")
	script := "#!/bin/sh\ncase \"$*\" in\n" +
		"  *--version*) echo '{\"v\":1,\"version\":\"9.9.9\",\"capabilities\":[\"agent-exec\",\"agent-scan\",\"org-json-v1\"]}' ;;\n" +
		"  *approvals*|*questions*|*gates*) echo '{\"v\":1,\"items\":[]}' ;;\n" +
		"  *status*) echo '{\"v\":1,\"name\":\"growth\",\"status\":\"running\",\"run\":\"r1\"}' ;;\n" +
		"  *) exit 2 ;;\nesac\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MONOMIND_BIN", bin)
	if err := daemonhb.Write(daemonhb.Heartbeat{PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}

	db, err := storage.NewDatabase(f.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a := orgdecide.Defaults("default", "growth")
	a.Level = orgdesign.LevelFull
	if err := orgdecide.NewStore(db.DB).Put(ctx, a, "cli"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO workflow_executions (id, workflow_id, status, trigger_type, trigger_data, profile_id) VALUES ('e-1', '` + f.plainWF + `', 'WAITING', 'org_tool', '{"org":{"name":"growth","role":"writer","automation":"sum"}}', 'default')`,
		`INSERT INTO hil_pending (id, execution_id, workflow_id, node_id, node_name, node_config, profile_id) VALUES
			('hil-send', 'e-1', '` + f.plainWF + `', 'h1', 'Review email', '{"outbound":true}', 'default'),
			('hil-plain', 'e-1', '` + f.plainWF + `', 'h2', 'Review sum', '{}', 'default')`,
	} {
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	m := f.mustRun(t, "autonomy", "needs-you", "growth")
	refs := map[string]bool{}
	for _, it := range m["items"].([]interface{}) {
		refs[it.(map[string]interface{})["ref"].(string)] = true
	}
	if !refs["hil-send"] || refs["hil-plain"] {
		t.Fatalf("needs-you at full with the daemon running = %v, want hil-send only", m["items"])
	}
}
