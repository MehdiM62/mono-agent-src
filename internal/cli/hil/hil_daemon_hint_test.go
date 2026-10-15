package hil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/daemonhb"
	"github.com/monoes/mono-agent/internal/storage"
)

// pendingIDs seeds one pending item per caption and returns their ids.
func pendingIDs(t *testing.T, cfg *testConfig, captions ...string) []string {
	t.Helper()
	sdb, err := storage.NewDatabase(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sdb.Close()
	seedPending(t, sdb.DB, captions...)
	rows, err := sdb.DB.Query(`SELECT id FROM hil_pending WHERE status = 'pending' ORDER BY created_at, rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// QA-A U4: with no daemon running, a decision was stored but the paused run
// never resumed, and nothing said so. approve/reject now say the run waits
// for a daemon; with a live daemon they print no hint.
func TestHILDecisionHintsWithoutDaemon(t *testing.T) {
	t.Setenv("MONOAGENT_DAEMON_HEARTBEAT", filepath.Join(t.TempDir(), "hb.json"))
	cfg := &testConfig{DBPath: newHILTestDB(t), ProfileID: "default"}
	ids := pendingIDs(t, cfg, "alpha", "bravo", "charlie")

	var err error
	out := captureStdout(t, func() {
		cmd := NewCommand(testDeps(cfg))
		cmd.SetArgs([]string{"reject", ids[0]})
		err = cmd.Execute()
	})
	if err != nil || !strings.Contains(out, "Rejected HIL item") || !strings.Contains(out, "monoagentcli daemon") {
		t.Fatalf("reject without a daemon: %v\n%s", err, out)
	}

	cfg.JSONOutput = true
	out = captureStdout(t, func() {
		cmd := NewCommand(testDeps(cfg))
		cmd.SetArgs([]string{"approve", ids[1]})
		err = cmd.Execute()
	})
	var rec map[string]string
	if err != nil || json.Unmarshal([]byte(out), &rec) != nil || rec["status"] != "approved" || rec["hint"] != noDaemonHint {
		t.Fatalf("approve --json without a daemon: %v\n%s", err, out)
	}

	if err := daemonhb.Write(daemonhb.Heartbeat{PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	cfg.JSONOutput = false
	out = captureStdout(t, func() {
		cmd := NewCommand(testDeps(cfg))
		cmd.SetArgs([]string{"approve", ids[2]})
		err = cmd.Execute()
	})
	if err != nil || !strings.Contains(out, "Approved HIL item") || strings.Contains(out, "Hint") {
		t.Fatalf("approve with a live daemon: %v\n%s", err, out)
	}
}
