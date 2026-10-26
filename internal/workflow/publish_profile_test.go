package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A change that creates a workflow carries its profile on the intent, so deleting the profile while
// the change is open is visible; and if the profile is gone at the commit, the commit is refused
// and the file goes back, instead of rows being written for a profile that no longer exists.
func TestPublishRefusesToCommitIntoAProfileDeletedMeanwhile(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	if _, err := s.db.Exec(`INSERT INTO profiles (id, name) VALUES ('doomed', 'Doomed')`); err != nil {
		t.Fatal(err)
	}
	var sawProfile string
	guardedCommitFault = func() error {
		_ = s.db.QueryRow(`SELECT profile_id FROM workflow_publications WHERE workflow_id = 'wf-new'`).Scan(&sawProfile)
		_, err := s.db.Exec(removeProfileSQL, "doomed")
		return err
	}
	w := mirrorWF("wf-new", "n-trig", "n-set")
	w.ProfileID = "doomed"
	err := h.SaveWorkflow(ctx, w)
	if !errors.Is(err, ErrNoProfile) {
		t.Fatalf("%v", err)
	}
	if sawProfile != "doomed" {
		t.Fatalf("the intent did not carry the profile: %q", sawProfile)
	}
	if _, statErr := os.Stat(filepath.Join(h.files.dir, "wf-new.json")); !os.IsNotExist(statErr) {
		t.Fatalf("the file of the refused workflow is still there: %v", statErr)
	}
	if got, _ := s.GetWorkflow(ctx, "wf-new"); got != nil || openIntentCount(t, s) != 0 {
		t.Fatal("rows or an intent were left behind")
	}
}

const removeProfileSQL = `DELETE FROM profiles WHERE id = ?`
