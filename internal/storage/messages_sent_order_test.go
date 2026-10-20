package storage

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/data"
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

// Migration 107 rewrites the stored sent_at values that carry a zone other
// than UTC to the same instant in UTC, to the nanosecond, and leaves every other value exactly as it
// was: already at +0000, other forms (RFC3339, unix seconds, empty, NULL)
// and one strftime cannot read.
func TestApplyMigration107_SentAtInUTC(t *testing.T) {
	db := databaseBelow107(t)
	for _, p := range []string{"p1", "p2"} {
		if _, err := db.DB.Exec(`INSERT INTO people (id, platform_username, platform, profile_id) VALUES (?, ?, 'email', 'pr1')`, p, p); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		person, id string
		before     any
		want       string // quote(sent_at) after; "" when unchanged
	}{
		// What the driver wrote for a time.Time (Go's time text), on p1.
		{"p1", "zoned", time.Date(2026, 10, 19, 12, 0, 0, 0, tehran).String(), "'2026-10-19 08:30:00'"}, // "… +0330 +0330"
		{"p1", "zoned-frac", time.Date(2026, 10, 19, 3, 4, 5, 250_000_000, time.FixedZone("", -4*3600)).String(), "'2026-10-19 07:04:05.25'"},
		{"p1", "zoned-nanos", time.Date(2026, 10, 19, 5, 0, 0, 123456789, time.FixedZone("IST", 5*3600+30*60)).String(), "'2026-10-18 23:30:00.123456789'"},
		{"p1", "local-zone", "2026-10-19 08:00:00 -0400 EDT", "'2026-10-19 12:00:00'"},
		{"p1", "local-zone-monotonic", "2026-10-19 08:00:00.5 -0400 EDT m=+0.000123", "'2026-10-19 12:00:00.5'"},
		{"p1", "local-zone-numeric-name", "2026-10-19 14:00:00 +0500 +05", "'2026-10-19 09:00:00'"},
		{"p1", "utc", "2026-10-19 09:10:00 +0000 UTC", ""},
		{"p1", "utc-numeric-name", "2026-10-19 09:20:00 +0000 +0000", ""},
		{"p1", "migrated", "2026-10-19 08:40:00.000", ""},
		// Forms our code does not write, on p2.
		{"p2", "rfc3339", "2026-10-19T09:00:00+03:30", ""},
		{"p2", "unix-seconds", int64(1760864400), ""},
		{"p2", "unix-seconds-real", 1760864400.5, ""},
		{"p2", "empty", "", ""},
		{"p2", "null", nil, ""},
		{"p2", "unreadable", "2026-13-45 12:00:00 +0330 +0330", ""},
		{"p2", "unreadable-offset", "2026-10-19 12:00:00 +03x0 +0330", ""},
		{"p2", "unreadable-fraction", "2026-10-19 12:00:00.5x +0330 +0330", ""},
	}
	before := map[string]string{}
	for _, c := range cases {
		if _, err := db.DB.Exec(`INSERT INTO person_messages (id, person_id, source, external_id, sent_at, profile_id) VALUES (?, ?, 'email', ?, ?, 'pr1')`, c.id, c.person, c.id, c.before); err != nil {
			t.Fatal(err)
		}
		before[c.id] = quotedSentAt(t, db, c.id)
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		want := c.want
		if want == "" {
			want = before[c.id]
		}
		if got := quotedSentAt(t, db, c.id); got != want {
			t.Errorf("%s: sent_at %s became %s, want %s", c.id, before[c.id], got, want)
		}
	}
	got, err := db.ListPersonMessages("p1", "", "pr1", 20, 0)
	if err != nil {
		t.Fatalf("listing p1 after the migration: %v", err)
	}
	wantOrder := []string{"local-zone-monotonic", "local-zone", "utc-numeric-name", "utc", "local-zone-numeric-name", "migrated", "zoned", "zoned-frac", "zoned-nanos"}
	if fmt.Sprint(messageIDs(got)) != fmt.Sprint(wantOrder) {
		t.Errorf("order after the migration = %v, want %v", messageIDs(got), wantOrder)
	}
}

// 107 is safe to run again, as after a run whose recording was lost: a value
// it rewrote no longer matches, so the second run changes no row.
func TestApplyMigration107_RunsAgainSafely(t *testing.T) {
	db := databaseBelow107(t)
	if _, err := db.DB.Exec(`INSERT INTO people (id, platform_username, platform, profile_id) VALUES ('p1', 'ada', 'email', 'pr1')`); err != nil {
		t.Fatal(err)
	}
	for i, at := range []any{
		time.Date(2026, 10, 19, 12, 0, 0, 0, tehran),
		time.Date(2026, 10, 19, 8, 0, 0, 0, time.FixedZone("EDT", -4*3600)),
		time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC),
		"2026-10-19 12:00:00 +0330 +0330", nil,
	} {
		id := fmt.Sprint("m", i)
		if _, err := db.DB.Exec(`INSERT INTO person_messages (id, person_id, source, external_id, sent_at, profile_id) VALUES (?, 'p1', 'email', ?, ?, 'pr1')`, id, id, at); err != nil {
			t.Fatal(err)
		}
	}
	sqlText, err := data.MigrationsFS.ReadFile("migrations/107_person_messages_sent_at_utc.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(string(sqlText)); err != nil { // the run that was not recorded
		t.Fatal(err)
	}
	once := dumpTable(t, db, "person_messages")
	if err := db.ApplyMigrations(); err != nil { // 107 runs again, now recorded
		t.Fatal(err)
	}
	if again := dumpTable(t, db, "person_messages"); fmt.Sprint(again) != fmt.Sprint(once) {
		t.Errorf("a second run changed rows:\n once  %v\n again %v", once, again)
	}
	res, err := db.DB.Exec(string(sqlText))
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n != 0 {
		t.Errorf("a third run rewrote %d rows, want 0", n)
	}
}

// A database of the previous release, with people and their messages written
// the way that release wrote them (sent_at bound as given, in any zone),
// upgrades through a normal ApplyMigrations: a snapshot is taken first, no
// row is lost, every column but a zoned sent_at is unchanged, and every
// message reads back at its instant, in instant order.
func TestMigration107_UpgradesThePreviousReleaseSchema(t *testing.T) {
	db := databaseBelow107(t)
	var stderr strings.Builder
	prev := migrateStderr
	migrateStderr = &stderr
	t.Cleanup(func() { migrateStderr = prev })

	for _, p := range [][]any{
		{"p-ada", "ada.lovelace", "linkedin", "Ada Lovelace", "Analyst · Engines", 12, "1.2K", 340, "Hi Ada, loved your note on engines.", 1, "reviewed", "Analyst", "https://example.com/in/ada", "London, UK", "Writes about engines.\nAnd poetry.", `{"languages":["en","fr"]}`, "person"},
		{"p-bob", "bob_builds", "x", "Bob Ng — 吴", "", 0, "", 0, "", 0, "", "", "", "", "", "", "person"},
		{"p-acme", "acme", "instagram", "Acme Ltd", "Tools", 512, "12.3K", 10, "", 0, "pending", "", "https://example.com/acme", "Berlin", "We make tools.", `{"followers_raw":"12,345"}`, "company"},
	} {
		if _, err := db.DB.Exec(`INSERT INTO people (id, platform_username, platform, full_name, headline, content_count, follower_count,
			following_count, introduction, is_verified, category, job_title, profile_url, location, about, profile_details, kind, profile_id)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'pr1')`, p...); err != nil {
			t.Fatal(err)
		}
	}
	created := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	type msg struct {
		id, person, source, direction, status string
		sentAt                                time.Time // zero: NULL
	}
	msgs := []msg{
		{"m-ada-mail", "p-ada", "gmail", "inbound", "sent", time.Date(2026, 10, 18, 21, 15, 0, 0, time.UTC)},
		{"m-ada-dm", "p-ada", "linkedin", "outbound", "sent", time.Date(2026, 10, 19, 0, 30, 0, 0, tehran)}, // 21:00 UTC, earlier
		{"m-bob-x", "p-bob", "x", "inbound", "sent", time.Date(2026, 10, 18, 20, 0, 0, 0, time.FixedZone("EDT", -4*3600))},
		{"m-bob-ig", "p-bob", "instagram", "inbound", "sent", time.Date(2026, 10, 19, 5, 0, 0, 123456789, time.FixedZone("IST", 5*3600+30*60))},
		{"m-bob-draft", "p-bob", "linkedin", "outbound", "pending_approval", time.Time{}},
		{"m-acme-note", "p-acme", "manual", "outbound", "sent", time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)},
	}
	for i, m := range msgs {
		var sentAt any
		if !m.sentAt.IsZero() {
			sentAt = m.sentAt
		}
		if _, err := db.DB.Exec(`INSERT INTO person_messages (id, person_id, source, external_id, direction, sender, subject, body,
			metadata, status, sent_at, profile_id, created_at, read_at, hil_ref) VALUES (?,?,?,?,?,?,?,?,?,?,?,'pr1',?,?,?)`,
			m.id, m.person, m.source, fmt.Sprint("ext-", i), m.direction, "sender@example.com", "Re: plans ✓",
			"Hello,\n\nquoted 'text' — and \"more\".", `{"thread":"t1"}`, m.status, sentAt, created, created.Add(time.Hour), fmt.Sprint("hil-", i)); err != nil {
			t.Fatal(err)
		}
	}
	people, messages := dumpTable(t, db, "people"), dumpTable(t, db, "person_messages")

	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stderr.String(), "snapshot before migrating") {
		t.Errorf("no snapshot before 107 ran; stderr: %s", stderr.String())
	}
	if got := dumpTable(t, db, "people"); fmt.Sprint(got) != fmt.Sprint(people) {
		t.Errorf("people changed:\n before %v\n after  %v", people, got)
	}
	after := dumpTable(t, db, "person_messages")
	if len(after) != len(messages) {
		t.Fatalf("%d messages after the migration, %d before", len(after), len(messages))
	}
	for id, row := range messages {
		for col, v := range row {
			if col != "sent_at" && after[id][col] != v {
				t.Errorf("%s.%s changed from %s to %s", id, col, v, after[id][col])
			}
		}
	}
	wantOrder := map[string][]string{
		"p-ada":  {"m-ada-mail", "m-ada-dm"},
		"p-bob":  {"m-bob-draft", "m-bob-x", "m-bob-ig"}, // the draft orders by created_at
		"p-acme": {"m-acme-note"},
	}
	for person, want := range wantOrder {
		got, err := db.ListPersonMessages(person, "", "pr1", 10, 0)
		if err != nil {
			t.Fatalf("listing %s: %v", person, err)
		}
		if fmt.Sprint(messageIDs(got)) != fmt.Sprint(want) {
			t.Errorf("%s: order %v, want %v", person, messageIDs(got), want)
		}
		for _, g := range got {
			for _, m := range msgs {
				if m.id == g.ID && !g.SentAt.Equal(m.sentAt) {
					t.Errorf("%s: sent_at read back as %v, want %v", g.ID, g.SentAt, m.sentAt)
				}
			}
		}
	}
}

// databaseBelow107 is a database with the schema of the release before 107
// (v0.151.2 shipped migrations up to 106).
func databaseBelow107(t *testing.T) *Database {
	t.Helper()
	db, err := NewDatabase(filepath.Join(t.TempDir(), "sent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Close() })
	applyMigrationsBelow(t, db, 107)
	return db
}

func quotedSentAt(t *testing.T, db *Database, id string) (q string) {
	t.Helper()
	if err := db.DB.QueryRow(`SELECT quote(sent_at) FROM person_messages WHERE id = ?`, id).Scan(&q); err != nil {
		t.Fatal(err)
	}
	return q
}

// dumpTable returns every row of table, by id, as its columns' SQL literals
// (quote()), so a comparison sees types and bytes, not driver conversions.
func dumpTable(t *testing.T, db *Database, table string) map[string]map[string]string {
	t.Helper()
	rows, err := db.DB.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatal(err)
	}
	var cols, quoted []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, c)
		quoted = append(quoted, "quote("+c+")")
	}
	rows.Close()
	rows, err = db.DB.Query(`SELECT id, ` + strings.Join(quoted, ", ") + ` FROM ` + table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]map[string]string{}
	for rows.Next() {
		var id string
		vals := make([]string, len(cols))
		dest := []any{&id}
		for i := range vals {
			dest = append(dest, &vals[i])
		}
		if err := rows.Scan(dest...); err != nil {
			t.Fatal(err)
		}
		row := map[string]string{}
		for i, c := range cols {
			row[c] = vals[i]
		}
		out[id] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func messageIDs(ms []*PersonMessage) (ids []string) {
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	return ids
}

// sent_at written by older code or by hand (unix seconds, REAL, empty, RFC3339,
// Go time text) lists without error, and the mix orders by instant.
func TestListPersonMessagesToleratesAnySentAtForm(t *testing.T) {
	db := newUpsertPersonTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO people (id, platform_username, platform, profile_id) VALUES ('p1', 'ada', 'email', 'pr1')`); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	rows := []struct {
		id string
		at any
	}{
		{"rfc3339-10", "2026-10-19T10:00:00Z"},
		{"gotext-11", "2026-10-19 11:00:00 +0000 UTC"},
		{"int-12", base.Add(3 * time.Hour).Unix()},
		{"real-13", float64(base.Add(4 * time.Hour).Unix()) + 0.5},
		{"rfc3339-zoned-0830", "2026-10-19T12:00:00+03:30"},
		{"empty", ""},
	}
	for _, r := range rows {
		if _, err := db.DB.Exec(`INSERT INTO person_messages (id, person_id, source, external_id, sent_at, created_at, profile_id) VALUES (?, 'p1', 'email', ?, ?, '2026-10-19 07:00:00 +0000 UTC', 'pr1')`, r.id, r.id, r.at); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"real-13", "int-12", "gotext-11", "rfc3339-10", "rfc3339-zoned-0830", "empty"}
	got, err := db.ListPersonMessages("p1", "", "pr1", 20, 0)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if fmt.Sprint(messageIDs(got)) != fmt.Sprint(want) {
		t.Errorf("order = %v, want %v", messageIDs(got), want)
	}
	all, err := db.ListAllPersonMessages("pr1", "", 20, 0)
	if err != nil || len(all) != len(rows) {
		t.Fatalf("listing all: %d rows, err %v", len(all), err)
	}
	if m, err := db.GetPersonMessage("empty"); err != nil || m == nil || !m.SentAt.IsZero() {
		t.Errorf("empty sent_at: %v %v", m, err)
	}
}
