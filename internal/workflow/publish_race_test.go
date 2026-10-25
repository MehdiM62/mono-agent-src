//go:build wfcrash && !windows

package workflow

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// helperActions are the writers a child process can run against the rig's workflow.
var helperActions = map[string]func(ctx context.Context, h *HybridWorkflowStore, cur *Workflow) error{
	"apply-fix": func(ctx context.Context, h *HybridWorkflowStore, cur *Workflow) error {
		nodes := append([]WorkflowNode(nil), cur.Nodes...)
		nodes[1].Config = map[string]interface{}{"k": "from-helper"}
		return h.SaveNodesGuarded(ctx, "wf-c", nodes, SaveGuard{ExpectHash: os.Getenv("WF_HELPER_HASH"), RevokeApprovals: true})
	},
	"save": func(ctx context.Context, h *HybridWorkflowStore, cur *Workflow) error {
		cur.Nodes[1].Config = map[string]interface{}{"k": "from-save-" + os.Getenv("WF_HELPER_VALUE")}
		return h.SaveWorkflow(ctx, cur)
	},
	"save-revoking": func(ctx context.Context, h *HybridWorkflowStore, cur *Workflow) error {
		cur.Nodes[1].Config = map[string]interface{}{"k": "from-revoking"}
		return h.SaveWorkflowRevoking(ctx, cur, "")
	},
}

// race starts two writers together (a barrier file releases both) and waits for them. A writer
// either wins or reports a refusal (exit 3); anything else is a failure.
func (r *crashRig) race(t *testing.T, round int, a, b string) (winners int) {
	t.Helper()
	barrier := filepath.Join(r.root, fmt.Sprintf("go-%d", round))
	env := []string{"WF_HELPER_BARRIER=" + barrier, fmt.Sprintf("WF_HELPER_VALUE=%d", round)}
	ca, cb := r.startHelper(t, a, env...), r.startHelper(t, b, env...)
	if err := os.WriteFile(barrier, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*exec.Cmd{ca, cb} {
		switch err := c.Wait(); {
		case err == nil:
			winners++
		default:
			if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 3 {
				t.Fatalf("a writer failed: %v", err)
			}
		}
	}
	return winners
}

// assertConsistent: the file, the rows and the version history tell one story.
func (r *crashRig) assertConsistent(t *testing.T) {
	t.Helper()
	r.assertFileMatchesRows(t)
	if n := r.openIntents(t); n != 0 {
		t.Fatalf("%d intents left open", n)
	}
	if left, _ := filepath.Glob(filepath.Join(r.root, "workflows", "*.tmp")); len(left) != 0 {
		t.Fatalf("temp files left behind: %v", left)
	}
	rows, _ := r.s.GetWorkflow(context.Background(), "wf-c")
	file, _ := r.h.files.GetWorkflow(context.Background(), "wf-c")
	var latest, count, distinct int
	if err := r.s.db.QueryRow(`SELECT COALESCE(MAX(version), 0), COUNT(*), COUNT(DISTINCT version) FROM workflow_versions WHERE workflow_id = 'wf-c'`).
		Scan(&latest, &count, &distinct); err != nil {
		t.Fatal(err)
	}
	if count != distinct || rows.Version != latest || file.Version != latest {
		t.Fatalf("versions disagree: rows %d file %d latest %d (%d records, %d distinct)", rows.Version, file.Version, latest, count, distinct)
	}
}

func (r *crashRig) raceRounds(t *testing.T, rounds int, writer string) {
	t.Helper()
	for i := 0; i < rounds; i++ {
		cur, _ := r.h.GetWorkflow(context.Background(), "wf-c")
		r.hash, _ = ReviewHash(cur)
		if w := r.race(t, i, "apply-fix", writer); w < 1 {
			t.Fatalf("round %d: nobody won", i)
		}
		r.assertConsistent(t)
	}
}

// R-series: each definition writer raced against apply-fix across two processes.
func TestRaceSaveAgainstApplyFix(t *testing.T) { newCrashRig(t).raceRounds(t, 8, "save") }

// A save that revokes approvals does so in the commit that writes the rows.
func TestRaceSaveRevokingAgainstApplyFix(t *testing.T) {
	r := newCrashRig(t)
	r.raceRounds(t, 4, "save-revoking")
	if n := approvals(t, r.s, "wf-c"); n != 0 {
		t.Fatalf("approvals survived a revoking save: %d", n)
	}
}

// A plain save killed between its file and its commit is rolled back like a guarded one.
func TestCrashKilledSaveAfterRenameIsRolledBack(t *testing.T) {
	r := newCrashRig(t)
	before := r.fileOnDisk(t)
	waitKilled(t, r.startHelper(t, "save", "MONOAGENT_WF_FAILPOINT=after-rename"))
	r.assertConsistent(t)
	if string(r.fileOnDisk(t)) != string(before) {
		t.Fatal("the file still holds the save that never committed")
	}
}
