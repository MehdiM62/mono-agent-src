package browserctl

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/monoes/mono-agent/internal/testdb"
)

// Approving a site a block covers is refused and leaves the item pending,
// with no allow rule; rejecting it still works.
func TestDecidingABlockedSiteLeavesItPending(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t).DB
	id, err := AskSite(ctx, db, "p1", KindOther, "https://www.news.example/a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdatePermissions(db, func(p *Permissions) error {
		_, _, err := p.SetSite("news.example", RuleBlock)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var be *SiteBlockedDecisionError
	if _, err := DecideSiteItem(ctx, db, "p1", id, true); !errors.As(err, &be) || be.Origin != "https://www.news.example" {
		t.Fatalf("approving a blocked site: %v", err)
	}
	if it, _ := GetSiteItem(ctx, db, "p1", id); it.Status != "pending" {
		t.Fatalf("the item moved: %+v", it)
	}
	if p, _ := LoadPermissions(db); len(p.Sited(RuleAllow)) != 0 {
		t.Fatalf("an allow rule was added: %+v", p.Sites)
	}
	if it, err := DecideSiteItem(ctx, db, "p1", id, false); err != nil || it.Status != "rejected" {
		t.Fatalf("rejecting it: %+v %v", it, err)
	}
}

// Two updates never lose each other's change: one that read before the
// other wrote is read again.
func TestUpdatePermissionsNeverLosesAChange(t *testing.T) {
	db := testdb.Open(t).DB
	add := func(origin string) error {
		_, err := UpdatePermissions(db, func(p *Permissions) error {
			_, _, err := p.SetSite(origin, RuleAllow)
			return err
		})
		return err
	}
	// A second writer lands between the first one's read and its write:
	// before the setting exists (an insert) and once it does (an update).
	interleave := func(first, second string) {
		afterPermissionsRead = func() {
			afterPermissionsRead = nil
			if err := add(second); err != nil {
				t.Error(err)
			}
		}
		if err := add(first); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterPermissionsRead = nil })
	interleave("https://a.example", "https://b.example")
	interleave("https://c.example", "https://d.example")
	if p, _ := LoadPermissions(db); len(p.Sited(RuleAllow)) != 4 {
		t.Fatalf("interleaved: %+v", p.Sites)
	}

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := add(fmt.Sprintf("https://w%d.example", i)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if p, _ := LoadPermissions(db); len(p.Sited(RuleAllow)) != 12 {
		t.Fatalf("concurrent: %d rules: %+v", len(p.Sited(RuleAllow)), p.Sites)
	}
}

// At most SiteAskLimit items wait per profile (an existing one is still
// reused), and a site the user said no to is not asked again for a while.
func TestAskSiteLimits(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t).DB
	first := ""
	for i := range SiteAskLimit {
		id, err := AskSite(ctx, db, "p1", KindOther, fmt.Sprintf("https://s%d.example/", i), nil)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = id
		}
	}
	var full *SiteAsksFullError
	if _, err := AskSite(ctx, db, "p1", KindOther, "https://one-more.example/", nil); !errors.As(err, &full) || full.Pending != SiteAskLimit {
		t.Fatalf("item %d: %v", SiteAskLimit+1, err)
	}
	if id, err := AskSite(ctx, db, "p1", KindOther, "https://s0.example/x", nil); err != nil || id != first {
		t.Fatalf("a waiting site is still reused: %q %v", id, err)
	}
	if _, err := AskSite(ctx, db, "p2", KindOther, "https://one-more.example/", nil); err != nil {
		t.Fatalf("another profile's limit: %v", err)
	}

	if _, err := DecideSiteItem(ctx, db, "p1", first, false); err != nil {
		t.Fatal(err)
	}
	var rej *SiteRejectedError
	if _, err := AskSite(ctx, db, "p1", KindOther, "https://s0.example/", nil); !errors.As(err, &rej) || rej.Origin != "https://s0.example" {
		t.Fatalf("asking again right after a no: %v", err)
	}
	if _, err := AskSite(ctx, db, "p1", KindCoder, "https://s0.example/", nil); err != nil {
		t.Fatalf("another kind may ask: %v", err)
	}
	if _, err := db.Exec(`UPDATE hil_pending SET updated_at = datetime('now', '-11 minutes') WHERE id = ?`, first); err != nil {
		t.Fatal(err)
	}
	if _, err := AskSite(ctx, db, "p1", KindOther, "https://s0.example/", nil); errors.As(err, &rej) {
		t.Fatalf("a no from 11 minutes ago still holds: %v", err)
	}
}

// Approving adds the allow rule first and marks the item approved only
// then. A failed rule write leaves the item pending; a failed status write
// after it leaves the rule (the user's intent) and approving again marks
// the item.
func TestApprovingWritesTheRuleBeforeTheStatus(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t).DB
	status := func(id string) string {
		it, err := GetSiteItem(ctx, db, "p1", id)
		if err != nil {
			t.Fatal(err)
		}
		return it.Status
	}
	allowed := func(origin string) bool {
		p, err := LoadPermissions(db)
		return err == nil && p.Rule(origin) == RuleAllow
	}

	// Order: while the rule is being written, the item is still pending.
	id, _ := AskSite(ctx, db, "p1", KindOther, "https://a.example/", nil)
	afterPermissionsRead = func() {
		afterPermissionsRead = nil
		if s := status(id); s != "pending" {
			t.Errorf("the item was %s before its rule was written", s)
		}
	}
	t.Cleanup(func() { afterPermissionsRead = nil })
	if it, err := DecideSiteItem(ctx, db, "p1", id, true); err != nil || it.Status != "approved" || !allowed("https://a.example") {
		t.Fatalf("approve: %+v %v", it, err)
	}

	// The rule write fails: the item stays pending, no rule.
	id, _ = AskSite(ctx, db, "p1", KindOther, "https://b.example/", nil)
	if _, err := db.Exec(`UPDATE configs SET config_data = '{' WHERE name = ?`, PermissionsSetting); err != nil {
		t.Fatal(err)
	}
	if _, err := DecideSiteItem(ctx, db, "p1", id, true); err == nil {
		t.Fatal("approved with an unreadable permissions row")
	}
	if s := status(id); s != "pending" {
		t.Fatalf("after a failed rule write the item is %s", s)
	}
	if err := SavePermissions(db, DefaultPermissions()); err != nil {
		t.Fatal(err)
	}
	if allowed("https://b.example") {
		t.Fatal("a rule was written")
	}

	// The status write fails after the rule is in: the rule stands, the
	// item stays pending, and approving again marks it.
	if _, err := db.Exec(`CREATE TRIGGER no_approve BEFORE UPDATE OF status ON hil_pending
		WHEN NEW.status = 'approved' BEGIN SELECT RAISE(ABORT, 'forced'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := DecideSiteItem(ctx, db, "p1", id, true); err == nil {
		t.Fatal("the status write was forced to fail")
	}
	if s := status(id); s != "pending" || !allowed("https://b.example") {
		t.Fatalf("after a failed status write: item %s, rule %v", s, allowed("https://b.example"))
	}
	if _, err := db.Exec(`DROP TRIGGER no_approve`); err != nil {
		t.Fatal(err)
	}
	if it, err := DecideSiteItem(ctx, db, "p1", id, true); err != nil || it.Status != "approved" {
		t.Fatalf("approving again: %+v %v", it, err)
	}
}
