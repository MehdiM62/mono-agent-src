package tasks

import (
	"errors"
	"testing"
)

// These tests pin rules that the tests of store_test.go leave open (found by
// mutation). Every number below is a literal on purpose: a test that reads
// MaxOpenTasks from the code under test cannot see the
// constant change.

func TestLimitsAreTheDocumentedNumbers(t *testing.T) {
	for _, c := range []struct {
		name      string
		got, want int
	}{
		{"open tasks per profile", MaxOpenTasks, 2000},
		{"source title runes", MaxSourceTitleRunes, 200},
		{"app name runes", MaxAppRunes, 100},
		{"actor name length", MaxNameLen, 64},
		{"client id length", MaxClientIDLen, 64},
		{"url bytes", MaxURLBytes, 2048},
		{"title runes", MaxTitleRunes, 200},
		{"notes bytes", MaxNotesBytes, 64 * 1024},
		{"position gap", positionGap, 1024},
	} {
		if c.got != c.want {
			t.Errorf("%s is %d, the spec says %d", c.name, c.got, c.want)
		}
	}
}

// Agents have no hourly allowance (the user removed it): many agent tasks
// created at one instant all land, and the operator and captures are not
// affected either.
func TestAgentsAddTasksWithoutAnHourlyLimit(t *testing.T) {
	s, _, _ := newTestStore(t)
	for i := 0; i < 50; i++ {
		if _, _, err := s.Add(bg, "default", AddInput{Title: "a"}, bot("b")); err != nil {
			t.Fatalf("agent task %d in the same instant: %v", i+1, err)
		}
	}
	for _, c := range []struct {
		name  string
		in    AddInput
		actor Actor
	}{
		{"operator in the app", AddInput{Title: "t", SourceKind: SourceApp}, human},
		{"chrome capture", AddInput{Title: "t", SourceKind: SourceChrome}, Actor{Kind: Capture, Name: SourceChrome}},
		{"os capture", AddInput{Title: "t", SourceKind: SourceOS}, Actor{Kind: Capture, Name: SourceOS}},
	} {
		if _, _, err := s.Add(bg, "default", c.in, c.actor); err != nil {
			t.Errorf("%s after 50 agent tasks: %v", c.name, err)
		}
	}
}

// 1999 open cards leave room for one, 2000 for none, whatever the columns the cards sit in;
// archived cards never count.
func TestOpenTaskLimitIsExactAndCountsEveryColumn(t *testing.T) {
	s, db, _ := newTestStore(t)
	other := addProfile(t, db, "p2")
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	columns := []string{"inbox", "ready", "in_progress", "review", "done"}
	for i := 0; i < 1999; i++ {
		if _, err := tx.Exec(`INSERT INTO tasks (profile_id, title, status, position, created_at, updated_at) VALUES ('default', 'seed', ?, ?, ?, ?)`, columns[i%5], i, rowTime, rowTime); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 300; i++ {
		if _, err := tx.Exec(`INSERT INTO tasks (profile_id, title, status, position, created_at, updated_at) VALUES ('default', 'old', 'archived', ?, ?, ?)`, i, rowTime, rowTime); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Add(bg, "default", AddInput{Title: "the 2000th"}, human); err != nil {
		t.Fatalf("with 1999 open cards: %v", err)
	}
	if _, _, err := s.Add(bg, "default", AddInput{Title: "the 2001st"}, human); !errors.Is(err, ErrLimit) {
		t.Fatalf("with 2000 open cards: %v, want ErrLimit", err)
	}
	if _, _, err := s.Add(bg, other, AddInput{Title: "elsewhere"}, human); err != nil {
		t.Errorf("another profile: %v", err)
	}
}
