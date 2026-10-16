//go:build windows

package storage

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func openLockFile(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// The second locker, on its own handle, must block until the first unlocks.
// A no-op lockFile would let it through at once and fail the "blocked" check.
func TestLockFileExBlocksSecondHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.migrate.lock")
	a, b := openLockFile(t, path), openLockFile(t, path)
	if err := lockFile(a); err != nil {
		t.Fatal(err)
	}
	got := make(chan error, 1)
	go func() { got <- lockFile(b) }()
	select {
	case err := <-got:
		t.Fatalf("second lock did not block (err=%v)", err)
	case <-time.After(500 * time.Millisecond):
	}
	if err := unlockFile(a); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-got:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("second lock never acquired after unlock")
	}
	unlockFile(b)
}

// TestLockHolderHelper takes the lock named by the env var, signals readiness
// through a file, and then sleeps until killed.
func TestLockHolderHelper(t *testing.T) {
	path := os.Getenv("LOCK_HOLDER_PATH")
	if path == "" {
		t.Skip("helper process only")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := lockFile(f); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".ready", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Minute)
}

// A holder killed mid-migration must not block the next acquirer: the kernel
// drops its lock. The helper really holds it first (the main process cannot
// take it within the probe window), so a no-op lockFile fails that check.
func TestLockFileExReleasedWhenHolderKilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "y.migrate.lock")
	c := exec.Command(os.Args[0], "-test.run=^TestLockHolderHelper$", "-test.count=1")
	c.Env = append(os.Environ(), "LOCK_HOLDER_PATH="+path)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Process.Kill(); c.Wait() })
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(path + ".ready"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("holder never took the lock")
		}
		time.Sleep(20 * time.Millisecond)
	}

	f := openLockFile(t, path)
	got := make(chan error, 1)
	go func() { got <- lockFile(f) }()
	select {
	case err := <-got:
		t.Fatalf("lock acquired while the holder is alive (err=%v)", err)
	case <-time.After(500 * time.Millisecond):
	}
	if err := c.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-got:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("lock not released after the holder was killed")
	}
	unlockFile(f)
}
