package statedir

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func perm(t *testing.T, p string) fs.FileMode {
	t.Helper()
	info, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func skipOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix modes do not apply on Windows")
	}
}

func TestMkdirAllAndWriteFileArePrivate(t *testing.T) {
	skipOnWindows(t)
	dir := filepath.Join(t.TempDir(), ".monoagent")
	file := filepath.Join(dir, "sub", "state.json")
	if err := WriteFile(file, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]fs.FileMode{dir: DirPerm, filepath.Dir(file): DirPerm, file: FilePerm} {
		if got := perm(t, p); got != want {
			t.Errorf("%s: mode %v, want %v", p, got, want)
		}
	}
}

func TestWriteFileTightensAnExistingFile(t *testing.T) {
	skipOnWindows(t)
	file := filepath.Join(t.TempDir(), "login-tab.json")
	if err := os.WriteFile(file, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(file, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if got := perm(t, file); got != FilePerm {
		t.Fatalf("mode %v, want %v", got, FilePerm)
	}
	if b, _ := os.ReadFile(file); string(b) != "new" {
		t.Fatalf("content %q", b)
	}
}

func TestFindExposedAndRepair(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	dir := filepath.Join(root, ".monoagent")
	if err := os.MkdirAll(filepath.Join(dir, "captures"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil { // beat the umask
		t.Fatal(err)
	}
	write := func(p string, mode fs.FileMode) {
		t.Helper()
		if err := os.WriteFile(p, nil, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
	}
	db := filepath.Join(dir, "monoagent.db")
	write(db, 0o644)
	write(db+"-wal", 0o644)
	write(filepath.Join(dir, "release-state.json"), 0o644)
	write(filepath.Join(dir, "helper.sh"), 0o755)
	write(filepath.Join(dir, "extension.token"), 0o600)
	nested := filepath.Join(dir, "captures", "page.html")
	write(nested, 0o644)
	outside := filepath.Join(root, "elsewhere.db")
	write(outside, 0o640)
	if err := os.Symlink(outside, filepath.Join(dir, "link.db")); err != nil {
		t.Fatal(err)
	}

	extra := append(DBFiles(db), DBFiles(outside)...)
	found, err := FindExposed(dir, extra...)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range found {
		got[e.Path] = true
	}
	want := []string{dir, db, db + "-wal", filepath.Join(dir, "release-state.json"), filepath.Join(dir, "helper.sh"), outside}
	if len(found) != len(want) {
		t.Fatalf("found %d items, want %d: %+v", len(found), len(want), found)
	}
	for _, p := range want {
		if !got[p] {
			t.Errorf("not reported: %s", p)
		}
	}

	fixed, err := Repair(dir, extra...)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixed) != len(want) {
		t.Fatalf("repaired %d, want %d", len(fixed), len(want))
	}
	for p, wantMode := range map[string]fs.FileMode{
		dir: 0o700, db: 0o600, db + "-wal": 0o600, filepath.Join(dir, "release-state.json"): 0o600,
		filepath.Join(dir, "helper.sh"): 0o700, // the owner keeps execute
		filepath.Join(dir, "extension.token"): 0o600, outside: 0o600,
		nested: 0o644, // below the top level: unreachable once dir is 0700
	} {
		if got := perm(t, p); got != wantMode {
			t.Errorf("%s: mode %v, want %v", p, got, wantMode)
		}
	}
	if again, err := FindExposed(dir, extra...); err != nil || len(again) != 0 {
		t.Fatalf("after repair: %+v, %v", again, err)
	}
}

func TestFindExposedMissingDir(t *testing.T) {
	found, err := FindExposed(filepath.Join(t.TempDir(), "nope"), DBFiles("")...)
	if err != nil || len(found) != 0 {
		t.Fatalf("%+v, %v", found, err)
	}
}
