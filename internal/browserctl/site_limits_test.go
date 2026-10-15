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
