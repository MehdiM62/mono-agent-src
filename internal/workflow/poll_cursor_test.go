package workflow

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/vault"
)

type pollEntry struct {
	id   string
	date *time.Time
}

// pollRun opens the cursor as one run would, offers entries and saves.
func pollRun(t *testing.T, ctx context.Context, opts PollCursorOptions, entries ...pollEntry) []string {
	t.Helper()
	c, err := OpenPollCursor(ctx, stateInput("wf1", "poll"), StateScopeNode, "cursor:feed", opts)
	if err != nil {
		t.Fatal(err)
	}
	var fresh []string
	for _, e := range entries {
		if c.IsNew(e.id, e.date) {
			fresh = append(fresh, e.id)
		}
	}
	if err := c.Save(ctx); err != nil {
		t.Fatal(err)
	}
	return fresh
}

func TestPollCursor_FirstRunEmitsUnlessBaseline(t *testing.T) {
	day := func(d int) *time.Time { v := time.Date(2026, 10, d, 9, 0, 0, 0, time.UTC); return &v }
	for _, baseline := range []bool{false, true} {
		t.Run(fmt.Sprint("baseline=", baseline), func(t *testing.T) {
			ctx := vault.ContextWithDB(context.Background(), newStateDB(t))
			opts := PollCursorOptions{Baseline: baseline}
			first := pollRun(t, ctx, opts, pollEntry{"b", day(13)}, pollEntry{"a", day(12)})
			if want := map[bool]string{false: "[b a]", true: "[]"}[baseline]; fmt.Sprint(first) != want {
				t.Fatalf("first run = %v, want %s", first, want)
			}
			// Same for both from here on: new ids not dated before the newest seen.
			got := pollRun(t, ctx, opts, pollEntry{"c", day(14)}, pollEntry{"b", day(13)}, pollEntry{"old", day(11)}, pollEntry{"undated", nil})
			if fmt.Sprint(got) != "[c undated]" {
				t.Fatalf("second run = %v, want [c undated]", got)
			}
		})
	}
}

// The stored shape is {newest, ids}: system.rss_read cursors written before
// PollCursor existed keep working.
func TestPollCursor_ReadsTheRSSCursorShape(t *testing.T) {
	ctx := vault.ContextWithDB(context.Background(), newStateDB(t))
	sd, _ := StaticDataFor(ctx, stateInput("wf1", "poll"), StateScopeNode)
	if err := sd.Set(ctx, "cursor:feed", map[string]interface{}{"newest": time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC), "ids": []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	early := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	if got := pollRun(t, ctx, PollCursorOptions{Baseline: true}, pollEntry{"a", nil}, pollEntry{"b", nil}, pollEntry{"x", &early}); fmt.Sprint(got) != "[b]" {
		t.Fatalf("got %v, want [b]", got)
	}
}

func TestPollCursor_MaxIDs(t *testing.T) {
	ctx := vault.ContextWithDB(context.Background(), newStateDB(t))
	opts := PollCursorOptions{MaxIDs: 2}
	pollRun(t, ctx, opts, pollEntry{"a", nil}, pollEntry{"b", nil}, pollEntry{"c", nil})
	if got := pollRun(t, ctx, opts, pollEntry{"a", nil}, pollEntry{"c", nil}); fmt.Sprint(got) != "[c]" {
		t.Fatalf("got %v, want [c] (only the first 2 ids are kept)", got)
	}
}

func TestPollBaseline(t *testing.T) {
	for cfg, want := range map[string]bool{"": false, "false": false, "true": true} {
		m := map[string]interface{}{}
		switch cfg {
		case "true":
			m["baseline"] = true
		case "false":
			m["baseline"] = false
		}
		if got := PollBaseline(m); got != want {
			t.Errorf("PollBaseline(%v) = %v", m, got)
		}
	}
}

// PrevNewest lets a source narrow its listing by date (Gmail's after:)
// while IsNew still drops the boundary entries already passed on.
func TestPollCursor_PrevNewest(t *testing.T) {
	ctx := vault.ContextWithDB(context.Background(), newStateDB(t))
	open := func() *PollCursor {
		c, err := OpenPollCursor(ctx, stateInput("wf1", "poll"), StateScopeNode, "cursor:feed", PollCursorOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if _, ok := open().PrevNewest(); ok {
		t.Fatal("PrevNewest with no stored cursor")
	}
	// Undated entries only: still no date to narrow by.
	pollRun(t, ctx, PollCursorOptions{}, pollEntry{"u", nil})
	if _, ok := open().PrevNewest(); ok {
		t.Fatal("PrevNewest after undated entries only")
	}

	at := func(sec int) *time.Time { v := time.Unix(1_790_000_000+int64(sec), 0).UTC(); return &v }
	pollRun(t, ctx, PollCursorOptions{}, pollEntry{"m2", at(10)}, pollEntry{"m1", at(5)})
	c := open()
	newest, ok := c.PrevNewest()
	if !ok || !newest.Equal(*at(10)) {
		t.Fatalf("PrevNewest = %v, %v; want %v", newest, ok, *at(10))
	}
	// The next listing from newest's second on repeats m2; only m3 is new.
	var fresh []string
	for _, e := range []pollEntry{{"m3", at(12)}, {"m2", at(10)}} {
		if c.IsNew(e.id, e.date) {
			fresh = append(fresh, e.id)
		}
	}
	if err := c.Save(ctx); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(fresh) != "[m3]" {
		t.Fatalf("narrowed run = %v, want [m3]", fresh)
	}
	if newest, _ := open().PrevNewest(); !newest.Equal(*at(12)) {
		t.Fatalf("PrevNewest after the narrowed run = %v", newest)
	}
}
