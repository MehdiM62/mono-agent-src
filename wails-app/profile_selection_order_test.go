package main

import (
	"context"
	"database/sql"
	"testing"

	"github.com/monoes/mono-agent/internal/profiledir"

	_ "modernc.org/sqlite"
)

// The first start selects the profile the extension bridge marks as default,
// for created_at values in both stored formats, in the same second or not.
func TestChooseProfileAgreesWithTheBridgeDefaultMarker(t *testing.T) {
	sets := map[string][][3]string{
		"same second, mixed formats":   {{"work", "work", "2026-10-16T06:04:34Z"}, {"default", "Default", "2026-10-16 06:04:34"}},
		"RFC3339 older than space fmt": {{"default", "Default", "2026-10-16 06:04:34"}, {"work", "work", "2026-10-16T06:04:33Z"}},
		"space format older":           {{"work", "work", "2026-10-16T06:04:35Z"}, {"default", "Default", "2026-10-16 06:04:34"}},
	}
	for name, rows := range sets {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`CREATE TABLE profiles (id TEXT PRIMARY KEY, name TEXT, created_at TEXT)`); err != nil {
			t.Fatal(err)
		}
		var gui []cliProfile
		for _, r := range rows {
			if _, err := db.Exec(`INSERT INTO profiles VALUES (?, ?, ?)`, r[0], r[1], r[2]); err != nil {
				t.Fatal(err)
			}
			gui = append(gui, prof(r[0], r[2]))
		}
		list, err := profiledir.List(context.Background(), db)
		db.Close()
		if err != nil || len(list) == 0 {
			t.Fatalf("%s: List = %v, %v", name, list, err)
		}
		got, err := chooseProfile("", gui)
		if err != nil || got != list[0].ID || !list[0].Default {
			t.Errorf("%s: app selects %q, bridge marks %q (%v)", name, got, list[0].ID, err)
		}
	}
}
