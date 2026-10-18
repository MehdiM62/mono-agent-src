package storage

import (
	"path/filepath"
	"testing"
	"time"
)

var tehran = time.FixedZone("", 3*3600+30*60) // what time.Parse(time.RFC3339, "…+03:30") gives

// A message sent in another zone is stored in UTC: it reads back (the
// driver cannot read "… +0330 +0330") and orders by its instant, 12:00 at
// +03:30 being 08:30 UTC, before 09:00 UTC.
func TestPersonMessageSentAtIsStoredInUTC(t *testing.T) {
	db := newUpsertPersonTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO people (id, platform_username, platform, profile_id) VALUES ('p1', 'ada', 'email', 'pr1')`); err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct {
		id string
		at time.Time
	}{
		{"earlier-in-tehran", time.Date(2026, 10, 19, 12, 0, 0, 0, tehran)},
		{"later-in-utc", time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)},
	} {
		msg := &PersonMessage{ID: m.id, PersonID: "p1", Source: "email", ExternalID: m.id, Direction: "inbound", SentAt: m.at}
		if err := db.UpsertPersonMessage(msg, "pr1"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.ListPersonMessages("p1", "", "pr1", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "later-in-utc" || got[1].ID != "earlier-in-tehran" {
		t.Fatalf("order = %v, want later-in-utc, earlier-in-tehran", messageIDs(got))
	}
	if want := time.Date(2026, 10, 19, 8, 30, 0, 0, time.UTC); !got[1].SentAt.Equal(want) {
		t.Errorf("sent_at read back as %v, want %v", got[1].SentAt, want)
	}
}

// Migration 107 rewrites the unreadable non-UTC sent_at values already stored
// to the UTC instant, and leaves every other value as it was.
func TestApplyMigration107_SentAtInUTC(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "sent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	applyMigrationsBelow(t, db, 107)
	if _, err := db.DB.Exec(`INSERT INTO people (id, platform_username, platform, profile_id) VALUES ('p1', 'ada', 'email', 'pr1')`); err != nil {
		t.Fatal(err)
	}
	before := map[string]any{
		"zoned":      time.Date(2026, 10, 19, 12, 0, 0, 0, tehran).String(), // "2026-10-19 12:00:00 +0330 +0330"
		"zoned-frac": time.Date(2026, 10, 19, 3, 4, 5, 250_000_000, time.FixedZone("", -4*3600)).String(),
		"utc":        time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC).String(), // readable: kept
		"rfc3339":    "2026-10-19T09:00:00+03:30",                            // not time.Time text: kept
		"none":       nil,
	}
	for id, at := range before {
		if _, err := db.DB.Exec(`INSERT INTO person_messages (id, person_id, source, external_id, sent_at, profile_id) VALUES (?, 'p1', 'email', ?, ?, 'pr1')`, id, id, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"zoned":      "2026-10-19 08:30:00.000",
		"zoned-frac": "2026-10-19 07:04:05.250",
		"utc":        before["utc"],
		"rfc3339":    before["rfc3339"],
		"none":       nil,
	}
	for id, w := range want {
		var got any
		if err := db.DB.QueryRow(`SELECT CAST(sent_at AS TEXT) FROM person_messages WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if s, ok := got.([]byte); ok {
			got = string(s)
		}
		if got != w {
			t.Errorf("%s: sent_at = %v, want %v", id, got, w)
		}
	}
	if _, err := db.ListPersonMessages("p1", "", "pr1", 10, 0); err != nil {
		t.Errorf("listing after the migration: %v", err)
	}
}

func messageIDs(ms []*PersonMessage) (ids []string) {
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	return ids
}
