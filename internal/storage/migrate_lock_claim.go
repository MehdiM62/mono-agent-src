package storage

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// claimLock is the flock-less migration lock used where flock does not
// exist: a claim file created with O_EXCL that records who holds it
// ("nonce pid unixnano"). It lives in an OS-independent file so the
// algorithm is testable everywhere; only the liveness check is per-OS.
//
// While it holds the claim the holder heartbeats: every heartbeat interval
// it atomically rewrites the claim with a fresh timestamp (same nonce). A
// claim is stale when its holder's PID is not alive or its last heartbeat is
// older than staleAfter, so a live holder is never judged stale by total age
// however long its migration runs, and a crashed holder whose PID has been
// reused by an unrelated process stops looking alive once the heartbeat
// stops. A stale claim is judged and removed under a short-lived break lock,
// so two waiters never both take ownership, and a release only removes a
// claim that still carries the releaser's own nonce.
type claimLock struct {
	path       string
	alive      func(pid int) bool
	staleAfter time.Duration
	heartbeat  time.Duration
	poll       time.Duration
	timeout    time.Duration
	pid        int

	stopHB chan struct{}
	hbDone chan struct{}
}

func claimBody(nonce string, pid int, at time.Time) string {
	return fmt.Sprintf("%s %d %d", nonce, pid, at.UnixNano())
}

func newNonce() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

// acquire blocks until the claim is ours and returns its nonce.
func (c *claimLock) acquire() (string, error) {
	nonce := newNonce()
	body := claimBody(nonce, c.pid, time.Now())
	deadline := time.Now().Add(c.timeout)
	for {
		h, err := os.OpenFile(c.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, werr := h.WriteString(body)
			cerr := h.Close()
			if werr != nil || cerr != nil {
				_ = os.Remove(c.path)
				return "", errors.Join(werr, cerr)
			}
			c.startHeartbeat(nonce)
			return nonce, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
		if c.breakIfStale() {
			continue
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("another process has held %s for %s", filepath.Base(c.path), c.timeout)
		}
		time.Sleep(c.poll)
	}
}

// breakIfStale removes the claim if it is stale and reports whether the
// claim slot is now free to try again.
func (c *claimLock) breakIfStale() bool {
	// Only one waiter at a time may judge and remove a claim: otherwise a
	// second waiter, having read the same stale claim, removes the fresh
	// claim the first waiter's acquirer has since created.
	if !c.tryBreakLock() {
		return false
	}
	defer c.unlockBreak()

	seen, err := os.ReadFile(c.path)
	if err != nil {
		// Vanished since the failed create: just retry.
		return errors.Is(err, os.ErrNotExist)
	}
	if !c.stale(string(seen)) {
		return false
	}
	return os.Remove(c.path) == nil
}

// tryBreakLock takes the break lock, an O_EXCL file dropped if its holder
// crashed. It does not wait.
func (c *claimLock) tryBreakLock() bool {
	bl := c.path + ".break"
	h, err := os.OpenFile(bl, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if info, serr := os.Stat(bl); serr == nil && time.Since(info.ModTime()) > breakLockAge {
			_ = os.Remove(bl)
		}
		return false
	}
	h.Close()
	return true
}

// lockBreak waits for the break lock, giving up after the lock timeout.
func (c *claimLock) lockBreak() bool {
	deadline := time.Now().Add(c.timeout)
	for !c.tryBreakLock() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(c.poll)
	}
	return true
}

func (c *claimLock) unlockBreak() { _ = os.Remove(c.path + ".break") }

// breakLockAge is how long a break lock may stand before its breaker is
// presumed crashed; breaking takes microseconds.
const breakLockAge = 30 * time.Second

func (c *claimLock) stale(body string) bool {
	f := strings.Fields(body)
	if len(f) == 3 {
		pid, perr := strconv.Atoi(f[1])
		ns, serr := strconv.ParseInt(f[2], 10, 64)
		if perr == nil && serr == nil {
			if time.Since(time.Unix(0, ns)) > c.staleAfter {
				return true
			}
			return !c.alive(pid)
		}
	}
	// Unparseable (e.g. a holder caught between create and write): only
	// the file's age can condemn it.
	info, err := os.Stat(c.path)
	return err == nil && time.Since(info.ModTime()) > c.staleAfter
}

// startHeartbeat keeps the claim fresh until stopHeartbeat.
func (c *claimLock) startHeartbeat(nonce string) {
	if c.heartbeat <= 0 {
		return
	}
	stop, done := make(chan struct{}), make(chan struct{})
	c.stopHB, c.hbDone = stop, done
	go func() {
		defer close(done)
		t := time.NewTicker(c.heartbeat)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if !c.refresh(nonce) {
					return
				}
			}
		}
	}()
}

func (c *claimLock) stopHeartbeat() {
	if c.stopHB == nil {
		return
	}
	close(c.stopHB)
	<-c.hbDone
	c.stopHB, c.hbDone = nil, nil
}

// refresh rewrites the claim with a fresh timestamp and the same nonce,
// atomically (temp file + rename), under the break lock so no waiter can
// break and replace the claim between the ownership check and the rename.
// It reports false once the claim is no longer ours; a transient failure
// returns true so the next tick retries.
func (c *claimLock) refresh(nonce string) bool {
	if !c.lockBreak() {
		return true
	}
	defer c.unlockBreak()
	body, err := os.ReadFile(c.path)
	if err != nil {
		return !errors.Is(err, os.ErrNotExist)
	}
	if f := strings.Fields(string(body)); len(f) == 0 || f[0] != nonce {
		return false
	}
	tmp := c.path + "." + nonce + ".hb"
	if err := writeSynced(tmp, claimBody(nonce, c.pid, time.Now())); err != nil {
		_ = os.Remove(tmp)
		return true
	}
	if err := os.Rename(tmp, c.path); err != nil {
		_ = os.Remove(tmp)
	}
	return true
}

func writeSynced(path, body string) error {
	h, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := h.WriteString(body)
	serr := h.Sync()
	return errors.Join(werr, serr, h.Close())
}

// release removes the claim only if it still carries our nonce.
func (c *claimLock) release(nonce string) error {
	c.stopHeartbeat()
	body, err := os.ReadFile(c.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if f := strings.Fields(string(body)); len(f) == 0 || f[0] != nonce {
		return nil
	}
	return os.Remove(c.path)
}
