package storage

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewDatabaseCreatesAPrivateFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix modes do not apply on Windows")
	}
	path := filepath.Join(t.TempDir(), "monoagent.db")
	db, err := NewDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	// A write creates the -wal and -shm files; SQLite gives them the
	// database file's mode.
	if _, err := db.DB.Exec(`CREATE TABLE t (x INTEGER)`); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s: mode %v, want 0600", filepath.Base(p), got)
		}
	}
}

func TestNewDatabaseKeepsAnExistingFilesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix modes do not apply on Windows")
	}
	path := filepath.Join(t.TempDir(), "shared.db")
	if err := os.WriteFile(path, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	db, err := NewDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("mode %v, want 0640 kept", got)
	}
}
