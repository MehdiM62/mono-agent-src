package fsconfine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A run of a workflow the user never approved writes no hidden file or
// folder in the user's home (one can run code later), except in the project
// folder; anywhere else it writes as before, and an approved run is
// unchanged.
func TestWritePathKeepsUnapprovedRunsOutOfHiddenHomeFiles(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	project := filepath.Join(home, ".monoagent", "profiles", "p1")
	for _, d := range []string{filepath.Join(home, "notes"), filepath.Join(home, ".config"), project} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.Symlink(filepath.Join(home, ".config"), filepath.Join(home, "notes", "cfg")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(home)
	guarded := GuardHiddenHome(context.Background(), project)

	for _, p := range []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".config", "autostart", "x.desktop"),
		filepath.Join(home, ".monoagent", "monoagent.db"),
		filepath.Join(home, "notes", "cfg", "systemd", "user", "x.service"), // through a link
		".profile", // relative to the working directory, home here
	} {
		if _, err := WritePath(guarded, p); !errors.Is(err, ErrHiddenHomeWrite) {
			t.Errorf("unapproved run, %s: err = %v, want the hidden-home refusal", p, err)
		}
		if _, err := WritePath(context.Background(), p); err != nil {
			t.Errorf("approved run, %s: %v", p, err)
		}
	}
	for _, p := range []string{
		filepath.Join(home, "notes", "out.csv"),
		filepath.Join(home, "new", "folder", "out.csv"),
		filepath.Join(project, ".monomind", "orgs", "x.json"), // hidden, but in the project folder
		filepath.Join(project, "exports", "out.csv"),
		filepath.Join(base, "elsewhere", ".hidden"), // outside the home
	} {
		if _, err := WritePath(guarded, p); err != nil {
			t.Errorf("unapproved run, %s: %v", p, err)
		}
	}
	// Reading is unchanged: Path never applies the rule.
	if _, err := Path(guarded, filepath.Join(home, ".bashrc")); err != nil {
		t.Errorf("Path: %v", err)
	}
}
