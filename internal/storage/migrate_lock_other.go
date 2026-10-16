//go:build !unix

package storage

import (
	"os"
	"sync"
	"time"
)

// Without flock the lock is the claim-file algorithm in migrate_lock_claim.go
// (chosen over LockFileEx so one tested implementation serves windows and
// every other non-unix target).
const (
	lockPollInterval = 20 * time.Millisecond
	lockWaitTimeout  = 120 * time.Second
	staleLockAge     = 5 * time.Minute
)

var (
	heldMu     sync.Mutex
	heldNonces = map[string]string{}
)

func lockFile(f *os.File) error {
	c := &claimLock{
		path: f.Name() + ".claim", alive: processAlive, maxAge: staleLockAge,
		poll: lockPollInterval, timeout: lockWaitTimeout, pid: os.Getpid(),
	}
	nonce, err := c.acquire()
	if err != nil {
		return err
	}
	heldMu.Lock()
	heldNonces[c.path] = nonce
	heldMu.Unlock()
	return nil
}

func unlockFile(f *os.File) error {
	path := f.Name() + ".claim"
	heldMu.Lock()
	nonce := heldNonces[path]
	delete(heldNonces, path)
	heldMu.Unlock()
	return (&claimLock{path: path}).release(nonce)
}
