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
	// A holder rewrites its claim every claimHeartbeat; a claim whose
	// last heartbeat is older than staleLockAge is abandoned.
	claimHeartbeat = 10 * time.Second
	staleLockAge   = 60 * time.Second
	// An empty or unparseable claim is left alone this long.
	claimEmptyGrace = 10 * time.Second
)

type heldClaim struct {
	lock  *claimLock
	nonce string
}

var (
	heldMu sync.Mutex
	held   = map[string]heldClaim{}
)

func lockFile(f *os.File) error {
	c := &claimLock{
		path: f.Name() + ".claim", alive: processAlive,
		staleAfter: staleLockAge, heartbeat: claimHeartbeat,
		emptyGrace: claimEmptyGrace, breakAge: breakLockAge,
		poll: lockPollInterval, timeout: lockWaitTimeout, pid: os.Getpid(),
	}
	nonce, err := c.acquire()
	if err != nil {
		return err
	}
	heldMu.Lock()
	held[c.path] = heldClaim{c, nonce}
	heldMu.Unlock()
	return nil
}

func unlockFile(f *os.File) error {
	path := f.Name() + ".claim"
	heldMu.Lock()
	h, ok := held[path]
	delete(held, path)
	heldMu.Unlock()
	if !ok {
		return nil
	}
	return h.lock.release(h.nonce)
}
