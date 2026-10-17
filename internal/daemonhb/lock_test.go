package daemonhb

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/monoes/mono-agent/internal/testhome"
)

// One daemon at a time: a second Lock fails while the first is held, and
// succeeds once it is released.
func TestLockIsSingleInstance(t *testing.T) {
	t.Setenv("MONOAGENT_DAEMON_HEARTBEAT", filepath.Join(t.TempDir(), "hb.json"))
	release, err := Lock()
	if err != nil {
		t.Fatal(err)
	}
	if !Locked() {
		t.Fatal("Locked() = false while held")
	}
	if _, err := Lock(); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Lock = %v, want ErrLocked", err)
	}
	release()
	if Locked() {
		t.Fatal("Locked() = true after release")
	}
	again, err := Lock()
	if err != nil {
		t.Fatalf("Lock after release: %v", err)
	}
	again()
}

// The lock holds across processes, which is the case that matters: a
// second `monoagentcli daemon` is another process.
func TestLockAcrossProcesses(t *testing.T) {
	if os.Getenv("DAEMONHB_LOCK_HELPER") == "1" {
		if _, err := Lock(); errors.Is(err, ErrLocked) {
			os.Exit(3)
		}
		os.Exit(0)
	}
	t.Setenv("MONOAGENT_DAEMON_HEARTBEAT", filepath.Join(t.TempDir(), "hb.json"))
	release, err := Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	// The helper exits from inside the test, so a test home of its own would
	// never be removed: it keeps the one it is given (testhome.KeepHomeEnv),
	// in a temp dir of its own where a leftover would show.
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run", "^TestLockAcrossProcesses$")
	cmd.Env = append(os.Environ(), "DAEMONHB_LOCK_HELPER=1",
		testhome.KeepHomeEnv+"=1", "HOME="+home, "USERPROFILE="+home, "TMPDIR="+tmp)
	err = cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("another process got the lock (err %v)", err)
	}
	if left, _ := filepath.Glob(filepath.Join(tmp, "monoagent-testhome-*")); len(left) > 0 {
		t.Fatalf("the helper left a test home behind: %v", left)
	}
}

// LockFile is the same lock for any path (the validation lock uses it); a
// held one gives ErrHeld, not the daemon's ErrLocked.
func TestLockFileHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	release, err := LockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockFile(path); !errors.Is(err, ErrHeld) {
		t.Fatalf("second LockFile = %v, want ErrHeld", err)
	}
	release()
	again, err := LockFile(path)
	if err != nil {
		t.Fatalf("LockFile after release: %v", err)
	}
	again()
}
