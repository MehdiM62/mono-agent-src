package extension

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// One lock for both state files (extension.pending.json and
// extension.browsers.json). The bridge and the CLI are different processes,
// and each read-modify-write used to be guarded by a process-local mutex only:
// a bridge write based on an older read could undo the owner's approval, or
// bring a denied claim back. Every read-modify-write now runs under stateMu
// (this process) and an exclusive file lock on extension.lock (the others).
//
// The lock is not re-entrant: a function that holds it calls only the
// *Locked variants.
var stateMu sync.Mutex

// lockState takes the state lock and returns the function that releases it.
func lockState() (unlock func(), err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	dir := filepath.Join(home, ".monoagent")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}
	stateMu.Lock()
	f, err := os.OpenFile(filepath.Join(dir, "extension.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		stateMu.Unlock()
		return nil, fmt.Errorf("open state lock: %w", err)
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		stateMu.Unlock()
		return nil, fmt.Errorf("take state lock: %w", err)
	}
	return func() {
		_ = unlockFile(f)
		_ = f.Close()
		stateMu.Unlock()
	}, nil
}
