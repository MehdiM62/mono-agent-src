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
// A claim is stale when its holder's PID is not alive, or when it is older
// than maxAge (guards against PID reuse and unparseable claims). A stale
// claim is broken by renaming it to a unique name and checking the renamed
// file is the one that was judged stale, so two waiters never both take
// ownership, and a release only removes a claim that still carries the
// releaser's own nonce.
type claimLock struct {
	path    string
	alive   func(pid int) bool
	maxAge  time.Duration
	poll    time.Duration
	timeout time.Duration
	pid     int
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
	body := fmt.Sprintf("%s %d %d", nonce, c.pid, time.Now().UnixNano())
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
	seen, err := os.ReadFile(c.path)
	if err != nil {
		// Vanished between create and read: just retry.
		return errors.Is(err, os.ErrNotExist)
	}
	if !c.stale(string(seen)) {
		return false
	}
	grave := c.path + ".stale." + newNonce()
	if err := os.Rename(c.path, grave); err != nil {
		return false
	}
	defer os.Remove(grave)
	got, err := os.ReadFile(grave)
	if err == nil && string(got) == string(seen) {
		return true
	}
	// We moved a claim that was replaced after we read it: put it back if
	// the slot is still free (Link fails if it is not).
	_ = os.Link(grave, c.path)
	return false
}

func (c *claimLock) stale(body string) bool {
	f := strings.Fields(body)
	if len(f) == 3 {
		pid, perr := strconv.Atoi(f[1])
		ns, serr := strconv.ParseInt(f[2], 10, 64)
		if perr == nil && serr == nil {
			if time.Since(time.Unix(0, ns)) > c.maxAge {
				return true
			}
			return !c.alive(pid)
		}
	}
	// Unparseable (e.g. a holder caught between create and write): only
	// the file's age can condemn it.
	info, err := os.Stat(c.path)
	return err == nil && time.Since(info.ModTime()) > c.maxAge
}

// release removes the claim only if it still carries our nonce.
func (c *claimLock) release(nonce string) error {
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
