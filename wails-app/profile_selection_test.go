package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// selectForTest selects a profile in memory only (no state file), for tests
// that need a profile to be selected.
func (a *App) selectForTest(id string) {
	a.selectedProfilePtr.Store(&id)
}

func prof(id, created string) cliProfile {
	return cliProfile{ID: id, Name: id, CreatedAt: created}
}

func TestChooseProfile(t *testing.T) {
	list := []cliProfile{
		prof("newer", "2026-03-01T00:00:00Z"),
		prof("b-oldest", "2026-01-01T00:00:00Z"),
		prof("a-oldest", "2026-01-01T00:00:00Z"),
	}
	cases := []struct {
		name, saved, want string
	}{
		{"a saved profile that exists wins", "newer", "newer"},
		{"nothing saved picks the oldest, ties by id", "", "a-oldest"},
		{"a deleted profile picks the oldest", "gone", "a-oldest"},
	}
	for _, c := range cases {
		got, err := chooseProfile(c.saved, list)
		if err != nil || got != c.want {
			t.Errorf("%s: chooseProfile(%q) = %q, %v; want %q", c.name, c.saved, got, err, c.want)
		}
	}
	if _, err := chooseProfile("anything", nil); !errors.Is(err, errNoProfile) {
		t.Errorf("no profiles: err = %v, want errNoProfile", err)
	}
}

func TestResolveSelectedProfilePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), guiStateFile)
	list := []cliProfile{prof("old", "2026-01-01T00:00:00Z"), prof("new", "2026-02-01T00:00:00Z")}

	// Missing file: the oldest is chosen and saved.
	if id, err := resolveSelectedProfile(path, list); err != nil || id != "old" {
		t.Fatalf("first start: %q, %v", id, err)
	}
	if got := loadSelectedProfile(path); got != "old" {
		t.Fatalf("saved = %q, want old", got)
	}
	// A saved choice survives a restart.
	if err := saveSelectedProfile(path, "new"); err != nil {
		t.Fatal(err)
	}
	if id, err := resolveSelectedProfile(path, list); err != nil || id != "new" {
		t.Fatalf("restart: %q, %v", id, err)
	}
	// The saved profile was deleted: the oldest replaces it, on disk too.
	if id, err := resolveSelectedProfile(path, list[:1]); err != nil || id != "old" {
		t.Fatalf("after delete: %q, %v", id, err)
	}
	if got := loadSelectedProfile(path); got != "old" {
		t.Fatalf("saved after delete = %q, want old", got)
	}
	// A damaged file is no choice at all.
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if id, err := resolveSelectedProfile(path, list); err != nil || id != "old" {
		t.Fatalf("damaged file: %q, %v", id, err)
	}
	// No profiles: an error, and nothing written for a profile.
	empty := filepath.Join(t.TempDir(), guiStateFile)
	if _, err := resolveSelectedProfile(empty, nil); !errors.Is(err, errNoProfile) {
		t.Fatalf("no profiles: %v", err)
	}
	if _, err := os.Stat(empty); !os.IsNotExist(err) {
		t.Fatalf("a state file was written with no profile: %v", err)
	}
}

func TestSelectionRoundTripAndNoProfileError(t *testing.T) {
	a := &App{dbPath: filepath.Join(t.TempDir(), "monoagent.db")}
	if _, err := a.profileID(); !errors.Is(err, errNoProfile) {
		t.Fatalf("before any selection: %v, want errNoProfile", err)
	}
	if got := a.selectedProfileOrEmpty(); got != "" {
		t.Fatalf("selectedProfileOrEmpty = %q", got)
	}
	if err := a.setSelectedProfile("work"); err != nil {
		t.Fatal(err)
	}
	if id, err := a.profileID(); err != nil || id != "work" {
		t.Fatalf("profileID = %q, %v", id, err)
	}
	// A new App over the same folder reads the same choice back.
	if got := loadSelectedProfile(guiStatePath(a.dbPath)); got != "work" {
		t.Fatalf("persisted = %q, want work", got)
	}
}

func TestBindingsErrorWithoutAProfile(t *testing.T) {
	a := &App{}
	if _, err := a.scopedArgs("task", "list"); !errors.Is(err, errNoProfile) {
		t.Errorf("scopedArgs: %v", err)
	}
	if got := a.rawCLI(0, "summary"); !strings.Contains(got, errNoProfile.Error()) {
		t.Errorf("rawCLI = %q, want the no-profile error", got)
	}
	if err := a.runMonoCLI("", nil, "task", "list"); !errors.Is(err, errNoProfile) {
		t.Errorf("runMonoCLI: %v", err)
	}
}
