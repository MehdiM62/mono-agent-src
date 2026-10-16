package storage

import (
	"crypto/rand"
	"crypto/sha256"
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
	emptyGrace time.Duration
	breakAge   time.Duration
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
		err := publishExclusive(c.path, body)
		if err == nil {
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
	token, ok := c.tryBreakLock()
	if !ok {
		return false
	}
	defer c.unlockBreak(token)

	seen, err := os.ReadFile(c.path)
	if err != nil {
		// Vanished since the failed create: just retry.
		return errors.Is(err, os.ErrNotExist)
	}
	if !c.stale(string(seen)) {
		return false
	}
	return c.removeIfMatches(c.path, string(seen))
}

// The break lock follows the claim's ownership discipline: its body carries
// its owner's nonce ("nonce pid unixnano"), it is published atomically and
// exclusively, its owner removes it only if it is still the one it took, and
// a crashed one is removed by exactly one waiter (see breakStaleBreakLock).

// tryBreakLock takes the break lock and returns its token (the body that
// identifies it). It does not wait. A break lock whose owner is dead or has
// stood for breakAge is presumed crashed and is removed for the next try.
func (c *claimLock) tryBreakLock() (string, bool) {
	bl := c.path + ".break"
	token := claimBody(newNonce(), c.pid, time.Now())
	err := publishExclusive(bl, token)
	if err == nil {
		return token, true
	}
	if errors.Is(err, os.ErrExist) {
		c.breakStaleBreakLock(bl)
	}
	return "", false
}

func (c *claimLock) breakStaleBreakLock(bl string) {
	seen, err := os.ReadFile(bl)
	if err != nil {
		return
	}
	stale := false
	if _, pid, ns, ok := parseClaim(string(seen)); ok {
		stale = time.Since(time.Unix(0, ns)) > c.breakAge || !c.alive(pid)
	} else if info, serr := os.Stat(bl); serr == nil {
		stale = time.Since(info.ModTime()) > c.breakAge
	}
	if !stale {
		return
	}
	// Removing by name cannot be made safe: between judging the crashed
	// lock and removing it, another waiter may have removed it and a third
	// published a live one, which a by-name removal would delete. So the
	// right to remove one particular crashed lock is itself claimed with an
	// exclusive marker named for that lock's body. Exactly one waiter wins
	// it; while it holds it nobody else may remove or replace the lock (a
	// new lock can only appear once the old one is gone), so the removal is
	// of exactly what was judged. Markers are kept, not removed, so a late
	// waiter holding the same stale read cannot win again; they are swept
	// once old.
	sum := sha256.Sum256(seen)
	marker := bl + "." + hex.EncodeToString(sum[:8]) + ".taken"
	if publishExclusive(marker, "taken") != nil {
		return
	}
	if again, err := os.ReadFile(bl); err == nil && string(again) == string(seen) {
		_ = os.Remove(bl)
	}
	sweepMarkers(bl, 10*c.breakAge)
}

func sweepMarkers(bl string, olderThan time.Duration) {
	old, _ := filepath.Glob(bl + ".*.taken")
	for _, m := range old {
		if info, err := os.Stat(m); err == nil && time.Since(info.ModTime()) > olderThan {
			_ = os.Remove(m)
		}
	}
}

// lockBreak waits for the break lock, giving up after the lock timeout.
func (c *claimLock) lockBreak() (string, bool) {
	deadline := time.Now().Add(c.timeout)
	for {
		if token, ok := c.tryBreakLock(); ok {
			return token, true
		}
		if time.Now().After(deadline) {
			return "", false
		}
		time.Sleep(c.poll)
	}
}

// unlockBreak drops the break lock only if it is still the one we took.
func (c *claimLock) unlockBreak(token string) {
	c.removeIfMatches(c.path+".break", token)
}

// removeIfMatches deletes path only if its body is still want, reporting
// whether the path is now gone because of it. It moves the file to a unique
// name first (an atomic step), re-reads what it actually moved, and puts it
// straight back if that is not what the caller judged, so a file replaced
// after the caller looked is never destroyed.
func (c *claimLock) removeIfMatches(path, want string) bool {
	gone := path + "." + newNonce() + ".gone"
	if err := os.Rename(path, gone); err != nil {
		return errors.Is(err, os.ErrNotExist)
	}
	got, err := os.ReadFile(gone)
	if err == nil && string(got) == want {
		_ = os.Remove(gone)
		return true
	}
	if err := linkFile(gone, path); err != nil && !errors.Is(err, os.ErrExist) {
		_ = os.Rename(gone, path)
	}
	_ = os.Remove(gone)
	return false
}

// breakLockAge is how long a break lock may stand before its breaker is
// presumed crashed; breaking takes microseconds.
const breakLockAge = 30 * time.Second

func (c *claimLock) stale(body string) bool {
	if _, pid, ns, ok := parseClaim(body); ok {
		if time.Since(time.Unix(0, ns)) > c.staleAfter {
			return true
		}
		return !c.alive(pid)
	}
	// Empty or unparseable (a holder that crashed mid-write on a filesystem
	// without hard links, or a truncated body): never judged from its
	// content, only from the file's age, and only after a short grace so a
	// claim being written right now is left alone.
	info, err := os.Stat(c.path)
	return err == nil && time.Since(info.ModTime()) > c.emptyGrace
}

// parseClaim reads "nonce pid unixnano". The timestamp must have the full
// width of a current UnixNano, so a body cut short mid-number is rejected
// rather than read as a moment in 1970.
func parseClaim(body string) (nonce string, pid int, ns int64, ok bool) {
	f := strings.Fields(body)
	if len(f) != 3 {
		return "", 0, 0, false
	}
	pid, perr := strconv.Atoi(f[1])
	ns, serr := strconv.ParseInt(f[2], 10, 64)
	if perr != nil || serr != nil || pid <= 0 || ns < minClaimNanos {
		return "", 0, 0, false
	}
	return f[0], pid, ns, true
}

// minClaimNanos is 2001-09-09, the first moment UnixNano has 19 digits.
const minClaimNanos = 1_000_000_000_000_000_000

// linkFile is os.Link; a test swaps it to exercise the no-hard-link path.
var linkFile = os.Link

// publishExclusive makes path appear with its complete body or not at all,
// and fails with os.ErrExist if it already exists. The body is written and
// synced to a unique temp file in the same directory and hard-linked into
// place, so a crash leaves at worst a stray temp file and never a visible
// empty or partial claim. Where hard links are unavailable it falls back to
// an O_EXCL create followed by the write, which stale() tolerates.
func publishExclusive(path, body string) error {
	tmp := path + "." + newNonce() + ".tmp"
	if err := writeSynced(tmp, body); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	defer os.Remove(tmp)
	err := linkFile(tmp, path)
	if err == nil || errors.Is(err, os.ErrExist) {
		return err
	}
	h, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := h.WriteString(body)
	serr := h.Sync()
	if err := errors.Join(werr, serr, h.Close()); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
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
	token, ok := c.lockBreak()
	if !ok {
		return true
	}
	defer c.unlockBreak(token)
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

// afterReleaseRead is a test seam between release's ownership check and its
// removal, where a claim could be broken and replaced.
var afterReleaseRead func()

// release removes the claim only if it still carries our nonce. The check
// and the removal happen under the break lock, so no waiter can break and
// replace the claim between them, and the removal itself re-verifies the
// body it checked, so a replaced claim is never deleted.
func (c *claimLock) release(nonce string) error {
	c.stopHeartbeat()
	token, ok := c.lockBreak()
	if !ok {
		return fmt.Errorf("could not take the break lock to release %s", filepath.Base(c.path))
	}
	defer c.unlockBreak(token)
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
	if afterReleaseRead != nil {
		afterReleaseRead()
	}
	c.removeIfMatches(c.path, string(body))
	return nil
}
