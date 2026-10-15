package browserjev

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// A run the chat started follows the chat's level in the user's browser
// permissions; a blocked site is refused at every level.
func TestRunFollowsItsKindsLevel(t *testing.T) {
	t.Setenv(EnvAICaller, "chat")
	const site = "https://shop.example.com/"

	// off: no browser at all, and no tab is opened.
	_, _, opened, err := levelRun(t, permissionsDB(t, browserctl.KindChat, browserctl.LevelOff), site, "", 30)
	if err == nil || opened || !strings.Contains(err.Error(), "browser access is off") {
		t.Fatalf("off: opened=%v err=%v", opened, err)
	}
	// full-no-approvals: any site, and no submit guard.
	res, drv, _, err := levelRun(t, permissionsDB(t, browserctl.KindChat, browserctl.LevelFullNoApprovals), site, "", 30)
	if err != nil || res["status"] != "done" || drv.armed {
		t.Fatalf("full-no-approvals: %v %v armed=%v", res, err, drv.armed)
	}
	// full: any site, behind the submit guard.
	res, drv, _, err = levelRun(t, permissionsDB(t, browserctl.KindChat, browserctl.LevelFull), site, "", 30)
	if err != nil || res["status"] != "done" || !drv.armed {
		t.Fatalf("full: %v %v armed=%v", res, err, drv.armed)
	}
	// dev: a real site needs an allow rule.
	if _, _, opened, err := levelRun(t, permissionsDB(t, browserctl.KindChat, browserctl.LevelDev), site, "", 30); err == nil || opened {
		t.Fatalf("dev without a rule: opened=%v err=%v", opened, err)
	}
	res, drv, _, err = levelRun(t, permissionsDB(t, browserctl.KindChat, browserctl.LevelDev, "https://shop.example.com=allow"), site, "", 30)
	if err != nil || res["status"] != "done" || !drv.armed {
		t.Fatalf("dev with an allow rule: %v %v", res, err)
	}
	// A block wins over every level, full-no-approvals included, also on a
	// page the run reaches.
	blocked := permissionsDB(t, browserctl.KindChat, browserctl.LevelFullNoApprovals, "https://bank.example.org=block")
	if _, _, _, err := levelRun(t, blocked, "https://bank.example.org/", "", 30); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("a blocked start: %v", err)
	}
	if res, _, _, err := levelRun(t, blocked, site, "https://bank.example.org/login", 30); err == nil || res != nil {
		t.Fatalf("a blocked page reached: %v %v", res, err)
	}
}

// Org-side runs follow the org's level; runs the user starts follow none.
func TestOrgAndUserRuns(t *testing.T) {
	ctx := permissionsDB(t, browserctl.KindOrg, browserctl.LevelOff)
	org := withOrgTrigger(ctx)
	if _, _, _, err := levelRun(t, org, "https://shop.example.com/", "", 30); err == nil || !strings.Contains(err.Error(), "browser access is off") {
		t.Fatalf("org at off: %v", err)
	}
	for _, kind := range browserctl.Kinds {
		ctx := permissionsDB(t, kind, browserctl.LevelOff)
		if res, drv, _, err := levelRun(t, ctx, "https://shop.example.com/", "", 30); err != nil || res["status"] != "done" || drv.armed {
			t.Fatalf("the user's run with %s off: %v %v", kind, res, err)
		}
	}
}

// pendingSite waits for the run's browser.site item and returns its id.
func pendingSite(t *testing.T, db *sql.DB) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var id string
		if db.QueryRow(`SELECT id FROM hil_pending WHERE workflow_id = ? AND status = 'pending'`, browserctl.SiteKind).Scan(&id) == nil {
			return id
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no browser.site item was raised")
	return ""
}

// ask: a new site raises an "allow this site?" item and the run waits for
// the user: allowed, it goes on; not allowed, it ends blocked; no answer
// before its timeout, it ends site_needs_approval with the item.
func TestAskLevelWaitsForTheUser(t *testing.T) {
	t.Setenv("CLAUDECODE", "1") // an agent shell: kind other
	for _, tc := range []struct {
		decide func(ctx context.Context, db *sql.DB, id string)
		status string
	}{
		{func(ctx context.Context, db *sql.DB, id string) {
			_, _ = browserctl.DecideSiteItem(ctx, db, "default", id, true)
		}, "done"},
		{func(ctx context.Context, db *sql.DB, id string) {
			_, _ = browserctl.DecideSiteItem(ctx, db, "default", id, false)
		}, "blocked"},
		{func(ctx context.Context, db *sql.DB, id string) {
			// An approval set outside `hil approve` adds no allow rule.
			_, _ = db.Exec(`UPDATE hil_pending SET status = 'approved' WHERE id = ?`, id)
		}, "blocked"},
	} {
		ctx := permissionsDB(t, browserctl.KindOther, browserctl.LevelAsk)
		db := vault.DBFromContext(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			tc.decide(ctx, db, pendingSite(t, db))
		}()
		res, _, opened, err := levelRun(t, ctx, "https://shop.example.com/", "", 30)
		<-done
		if err != nil || res["status"] != tc.status || res["hil_id"] == nil && tc.status != "done" {
			t.Fatalf("want %s: %v %v", tc.status, res, err)
		}
		if tc.status == "done" != opened {
			t.Errorf("a tab opens only on an allowed site: opened=%v", opened)
		}
	}

	ctx := permissionsDB(t, browserctl.KindOther, browserctl.LevelAsk)
	res, _, opened, err := levelRun(t, ctx, "https://shop.example.com/", "", 1)
	if err != nil || res["status"] != "site_needs_approval" || res["hil_id"] == nil || opened {
		t.Fatalf("no answer in time: %v %v opened=%v", res, err, opened)
	}
	id := res["hil_id"].(string)
	if again, _, _, _ := levelRun(t, ctx, "https://shop.example.com/", "", 1); again["hil_id"] != id {
		t.Errorf("asking again reuses the pending item: %v vs %s", again["hil_id"], id)
	}
}

func withOrgTrigger(ctx context.Context) context.Context {
	return workflow.WithTrigger(ctx, workflow.TriggerTypeOrgTool, map[string]interface{}{"org": map[string]interface{}{"name": "growth", "role": "writer"}})
}
